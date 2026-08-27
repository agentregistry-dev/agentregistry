package controller

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/logging"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

var logger = logging.New("registry-controller")

const (
	// defaultControllerResyncInterval is the repair cadence. LISTEN wakeups
	// handle normal event-driven scheduling; the minute tick bounds missed
	// notifications and retention-gap recovery without constantly scanning.
	defaultControllerResyncInterval = time.Minute
	// defaultWakeupReconnectDelay backs off LISTEN reconnects after transient DB
	// connection failures so the controller does not hot-loop.
	defaultWakeupReconnectDelay = 5 * time.Second
)

// ControllerHandle owns the always-on Deployment controller loops.
type ControllerHandle struct {
	Controller *DeploymentController
	Discovery  *DeploymentDiscoveryController
	Retention  *RetentionPruner
	// done closes after term-scoped controller and discovery workers both exit.
	done <-chan struct{}
}

// ControllerConfig controls optional controller maintenance loops.
type ControllerConfig struct {
	Retention                  RetentionPolicy
	DiscoveryInterval          time.Duration
	DiscoveryStaleAfterMisses  int
	DiscoveryDeleteAfterMisses int
	DependencyKinds            map[string]bool
	// Leadership supplies a context for every interval in which this process owns
	// Deployment and discovery reconciliation. Cancellation stops all provider
	// mutations before a subsequent owner starts.
	Leadership <-chan context.Context
	// DeploymentFinalized runs after required cleanup and finalizer release so a
	// parent Runtime can promptly re-evaluate whether all children are gone.
	DeploymentFinalized func(context.Context, *v1alpha1.Deployment)
}

// StartDeploymentController constructs the Deployment controller, runs the
// initial refresh synchronously, and starts reconcile/execution loops in the
// background. The returned handle is useful in tests and future health wiring.
func StartDeploymentController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
	discoverySources map[string]types.DeploymentDiscoverySource,
	config ControllerConfig,
) (*ControllerHandle, error) {
	if pool == nil {
		return nil, nil
	}
	if len(stores) == 0 {
		return nil, errors.New("deployment controller: stores are required")
	}

	// Retention runs once for the process instead of restarting on every leadership
	// term. It only prunes expired control-plane events, so it can keep history
	// bounded even while no leader is available without performing provider
	// mutations or competing with the elected reconciler.
	controlPlaneEventStore := v1alpha1store.NewControlPlaneEventStore(pool, pkgdb.MustNewSchema(pkgdb.OSSSchema))
	retention := &RetentionPruner{
		Stores: PruneStores{ControlPlaneEvents: controlPlaneEventStore},
		Policy: config.Retention,
	}
	if retention.Enabled() {
		go func() {
			if err := retention.Run(ctx, defaultRetentionPruneInterval); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("deployment controller retention pruner stopped", "error", err)
			}
		}()
	}
	if config.Leadership != nil {
		go runDeploymentControllerLeadership(ctx, pool, stores, adapters, discoverySources, config)
		return &ControllerHandle{Retention: retention}, nil
	}

	handle, err := startDeploymentControllerTerm(ctx, pool, stores, adapters, discoverySources, config)
	if err != nil {
		return nil, err
	}
	handle.Retention = retention
	return handle, nil
}

// startDeploymentControllerTerm builds and starts the controller and discovery
// workers owned by ctx. Each leadership term needs fresh queues, checkpoints,
// and wakeup subscriptions because canceled workers cannot be safely reused.
// If either worker exits, it cancels the generation so its sibling cannot leave
// the process in a partially running reconciliation state.
// The returned done channel lets the leadership loop wait for both workers to
// exit before it accepts another term, preventing overlapping provider calls
// and status writes from successive leaders.
func startDeploymentControllerTerm(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
	discoverySources map[string]types.DeploymentDiscoverySource,
	config ControllerConfig,
) (*ControllerHandle, error) {
	generationCtx, cancelGeneration := context.WithCancel(ctx)
	controlPlaneEventStore := v1alpha1store.NewControlPlaneEventStore(pool, pkgdb.MustNewSchema(pkgdb.OSSSchema))
	controller := &DeploymentController{
		Stores:              stores,
		Adapters:            adapters,
		Getter:              internaldb.NewGetter(stores),
		Events:              controlPlaneEventStore,
		DependencyKinds:     config.DependencyKinds,
		DeploymentFinalized: config.DeploymentFinalized,
	}
	if _, err := controller.Refresh(generationCtx); err != nil {
		cancelGeneration()
		return nil, fmt.Errorf("deployment controller initial refresh: %w", err)
	}
	controller.Wakeups = controlPlaneWakeups(generationCtx, pool)
	discovery := &DeploymentDiscoveryController{
		Stores:            stores,
		Sources:           discoverySources,
		StaleAfterMisses:  config.DiscoveryStaleAfterMisses,
		DeleteAfterMisses: config.DiscoveryDeleteAfterMisses,
	}

	handle := &ControllerHandle{Controller: controller, Discovery: discovery}
	handle.done = runDeploymentControllerWorkers(
		generationCtx, cancelGeneration, controller, discovery, config.DiscoveryInterval,
	)
	return handle, nil
}

// runDeploymentControllerWorkers runs reconciliation and discovery as one
// generation. When either worker returns, it cancels their shared context so
// the sibling and its wakeup subscription also stop. The returned channel
// closes only after both workers exit, allowing leadership handoff or restart
// without leaving a partially running generation behind.
func runDeploymentControllerWorkers(
	ctx context.Context,
	cancel context.CancelFunc,
	controller *DeploymentController,
	discovery *DeploymentDiscoveryController,
	discoveryInterval time.Duration,
) <-chan struct{} {
	done := make(chan struct{})
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		defer cancel()
		if err := controller.Run(ctx, defaultControllerResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("deployment controller stopped", "error", err)
		}
	}()
	go func() {
		defer workers.Done()
		defer cancel()
		if err := discovery.Run(ctx, discoveryInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("deployment discovery controller stopped", "error", err)
		}
	}()
	go func() {
		workers.Wait()
		close(done)
	}()
	return done
}

// runDeploymentControllerLeadership turns externally elected leadership terms
// into controller worker lifetimes. It retries initialization while the term is
// valid and restarts a generation that stops unexpectedly. Application shutdown
// cancels the term, and the loop waits for every worker to stop before consuming
// the next term so only one generation can mutate Deployments or provider
// resources at a time.
func runDeploymentControllerLeadership(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
	discoverySources map[string]types.DeploymentDiscoverySource,
	config ControllerConfig,
) {
	for {
		select {
		case <-ctx.Done():
			return
		case leadershipCtx, ok := <-config.Leadership:
			if !ok {
				return
			}
			termCtx, cancel := context.WithCancel(leadershipCtx)
			stop := context.AfterFunc(ctx, cancel)
		term:
			for termCtx.Err() == nil {
				handle, err := startDeploymentControllerTerm(termCtx, pool, stores, adapters, discoverySources, config)
				if err != nil {
					if !errors.Is(err, context.Canceled) {
						logger.Error("start deployment controller leadership", "error", err, "retry_after", defaultWakeupReconnectDelay)
					}
				} else {
					select {
					case <-termCtx.Done():
						<-handle.done
						break term
					case <-handle.done:
						if termCtx.Err() != nil {
							break term
						}
						logger.Error("deployment controller leadership generation stopped; restarting", "retry_after", defaultWakeupReconnectDelay)
					}
				}
				if !waitForReconnect(termCtx, defaultWakeupReconnectDelay) {
					break
				}
			}
			stop()
			cancel()
		}
	}
}

func controlPlaneWakeups(ctx context.Context, pool *pgxpool.Pool) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go runControlPlaneWakeupLoop(ctx, ch, func(ctx context.Context, wakeups chan<- struct{}) error {
		return listenForControlPlaneWakeups(ctx, pool, wakeups)
	}, defaultWakeupReconnectDelay)
	return ch
}

type controlPlaneListenFunc func(context.Context, chan<- struct{}) error

func runControlPlaneWakeupLoop(ctx context.Context, wakeups chan<- struct{}, listen controlPlaneListenFunc, reconnectDelay time.Duration) {
	for {
		err := listen(ctx, wakeups)
		if err == nil || errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return
		}
		logger.Error("deployment controller control-plane listener stopped; reconnecting", "error", err, "retry_after", reconnectDelay)
		if !waitForReconnect(ctx, reconnectDelay) {
			return
		}
	}
}

func listenForControlPlaneWakeups(ctx context.Context, pool *pgxpool.Pool, wakeups chan<- struct{}) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire LISTEN connection: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+v1alpha1store.ControlPlaneNotifyChannel); err != nil {
		return fmt.Errorf("listen for control-plane changes: %w", err)
	}
	for {
		if _, err := conn.Conn().WaitForNotification(ctx); err != nil {
			return fmt.Errorf("wait for control-plane notification: %w", err)
		}
		select {
		case wakeups <- struct{}{}:
		default:
		}
	}
}

func waitForReconnect(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

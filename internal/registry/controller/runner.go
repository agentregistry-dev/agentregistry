package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
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
}

// ControllerConfig controls optional controller maintenance loops.
type ControllerConfig struct {
	Retention RetentionPolicy
}

// StartDeploymentController constructs the Deployment controller, runs the
// initial refresh synchronously, and starts reconcile/execution loops in the
// background. The returned handle is useful in tests and future health wiring.
func StartDeploymentController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
	config ControllerConfig,
) (*ControllerHandle, error) {
	if pool == nil {
		return nil, nil
	}
	if len(stores) == 0 {
		return nil, errors.New("deployment controller: stores are required")
	}

	controlPlaneEventStore := v1alpha1store.NewControlPlaneEventStore(pool, pkgdb.MustNewSchema(pkgdb.OSSSchema))
	controller := &DeploymentController{
		Stores:   stores,
		Adapters: adapters,
		Getter:   internaldb.NewGetter(stores),
		Events:   controlPlaneEventStore,
	}
	if _, err := controller.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("deployment controller initial refresh: %w", err)
	}
	controller.Wakeups = controlPlaneWakeups(ctx, pool)
	discovery := &DeploymentDiscoveryController{
		Stores:   stores,
		Adapters: adapters,
	}

	retention := &RetentionPruner{
		Stores: PruneStores{
			ControlPlaneEvents: controlPlaneEventStore,
		},
		Policy: config.Retention,
	}
	handle := &ControllerHandle{Controller: controller, Discovery: discovery, Retention: retention}

	go func() {
		if err := controller.Run(ctx, defaultControllerResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("deployment controller stopped", "error", err)
		}
	}()
	go func() {
		if err := discovery.Run(ctx, defaultControllerResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("deployment discovery controller stopped", "error", err)
		}
	}()
	if retention.Enabled() {
		go func() {
			if err := retention.Run(ctx, defaultRetentionPruneInterval); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("deployment controller retention pruner stopped", "error", err)
			}
		}()
	}
	return handle, nil
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

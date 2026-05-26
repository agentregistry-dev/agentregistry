package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	defaultProjectorResyncInterval = time.Minute
	defaultExecutorInterval        = time.Second
	defaultExecutorBatchLimit      = 10
	defaultWakeupReconnectDelay    = 5 * time.Second
)

// Runtime owns the always-on Deployment controller loops.
type Runtime struct {
	Projector *Projector
	Executor  *DeploymentExecutor
	Retention *RetentionPruner
}

// RuntimeConfig controls optional controller maintenance loops.
type RuntimeConfig struct {
	Retention RetentionPolicy
}

// StartDeploymentController constructs the source projector, runs the initial
// refresh synchronously, and starts projection/execution loops in the
// background. The returned Runtime is useful in tests and future health wiring.
func StartDeploymentController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
	initialFinalizers map[string]func(v1alpha1.Object) []string,
	config RuntimeConfig,
) (*Runtime, error) {
	if pool == nil {
		return nil, nil
	}
	if len(stores) == 0 {
		return nil, errors.New("deployment controller: stores are required")
	}

	workStore := v1alpha1store.NewReconcileWorkStore(pool)
	reconcileEventStore := v1alpha1store.NewReconcileEventStore(pool)
	controlPlaneEventStore := v1alpha1store.NewControlPlaneEventStore(pool)
	sources := NewSourceIndex(stores, SourceIndexOptions{InitialFinalizers: initialFinalizers})
	deriver := &DeploymentWorkDeriver{Sources: sources, Work: workStore}
	projector := &Projector{
		Events: controlPlaneEventStore,
		FullResync: func(ctx context.Context) error {
			if err := sources.Refresh(ctx); err != nil {
				return err
			}
			_, err := deriver.DeriveAll(ctx)
			return err
		},
		ApplyEvent: func(ctx context.Context, event v1alpha1store.ControlPlaneEvent) error {
			if err := sources.ApplyEvent(ctx, event); err != nil {
				return err
			}
			_, err := deriver.DeriveAll(ctx)
			return err
		},
	}
	if _, err := projector.Refresh(ctx); err != nil {
		return nil, fmt.Errorf("deployment controller initial refresh: %w", err)
	}
	projector.Wakeups = controlPlaneWakeups(ctx, pool)

	executor := &DeploymentExecutor{
		Stores:   stores,
		Adapters: adapters,
		Getter:   internaldb.NewGetter(stores),
		Work:     workStore,
		Events:   reconcileEventStore,
	}
	retention := &RetentionPruner{
		Stores: PruneStores{
			ControlPlaneEvents: controlPlaneEventStore,
			ReconcileWork:      workStore,
			ReconcileAttempts:  reconcileEventStore,
		},
		Policy: config.Retention,
	}
	runtime := &Runtime{Projector: projector, Executor: executor, Retention: retention}

	go func() {
		if err := projector.Run(ctx, defaultProjectorResyncInterval); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("deployment controller projector stopped", "error", err)
		}
	}()
	go func() {
		if err := executor.Run(ctx, defaultExecutorInterval, defaultExecutorBatchLimit); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("deployment controller executor stopped", "error", err)
		}
	}()
	if retention.Enabled() {
		go func() {
			if err := retention.Run(ctx, defaultRetentionPruneInterval); err != nil && !errors.Is(err, context.Canceled) {
				slog.Error("deployment controller retention pruner stopped", "error", err)
			}
		}()
	}
	return runtime, nil
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
		slog.Error("deployment controller control-plane listener stopped; reconnecting", "error", err, "retry_after", reconnectDelay)
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

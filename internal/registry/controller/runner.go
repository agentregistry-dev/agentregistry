package controller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultProjectorResyncInterval = time.Minute
	defaultExecutorInterval        = time.Second
	defaultExecutorBatchLimit      = 10
)

// Runtime owns the always-on Deployment controller loops.
type Runtime struct {
	Projector *Projector
	Executor  *DeploymentExecutor
}

// StartDeploymentController constructs the source projector, runs the initial
// refresh synchronously, and starts projection/execution loops in the
// background. The returned Runtime is useful in tests and future health wiring.
func StartDeploymentController(
	ctx context.Context,
	pool *pgxpool.Pool,
	stores map[string]*v1alpha1store.Store,
	adapters map[string]types.DeploymentAdapter,
) (*Runtime, error) {
	if pool == nil {
		return nil, nil
	}
	if len(stores) == 0 {
		return nil, errors.New("deployment controller: stores are required")
	}

	workStore := v1alpha1store.NewReconcileWorkStore(pool)
	eventStore := v1alpha1store.NewReconcileEventStore(pool)
	sources := NewDeploymentSources(stores)
	deriver := &DeploymentWorkDeriver{Sources: sources, Work: workStore}
	projector := &Projector{
		Events: v1alpha1store.NewControlPlaneEventStore(pool),
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
		Events:   eventStore,
	}
	runtime := &Runtime{Projector: projector, Executor: executor}

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
	return runtime, nil
}

func controlPlaneWakeups(ctx context.Context, pool *pgxpool.Pool) <-chan struct{} {
	ch := make(chan struct{}, 1)
	go func() {
		conn, err := pool.Acquire(ctx)
		if err != nil {
			slog.Error("deployment controller failed to acquire LISTEN connection", "error", err)
			return
		}
		defer conn.Release()
		if _, err := conn.Exec(ctx, "LISTEN "+v1alpha1store.ControlPlaneNotifyChannel); err != nil {
			slog.Error("deployment controller failed to LISTEN for control-plane changes", "error", err)
			return
		}
		for {
			_, err := conn.Conn().WaitForNotification(ctx)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					slog.Error("deployment controller control-plane listener stopped", "error", err)
				}
				return
			}
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	}()
	return ch
}

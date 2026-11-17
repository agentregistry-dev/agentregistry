package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDeploymentControllerTermStopsWhenEitherWorkerExits(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := runDeploymentControllerWorkers(
		ctx,
		cancel,
		&DeploymentController{},
		&DeploymentDiscoveryController{},
		time.Hour,
	)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("controller generation did not stop after one worker exited")
	}
}

func TestControlPlaneWakeupLoopReconnectsAfterListenerError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	wakeups := make(chan struct{}, 1)
	calls := 0

	runControlPlaneWakeupLoop(ctx, wakeups, func(context.Context, chan<- struct{}) error {
		calls++
		if calls == 1 {
			return errors.New("connection dropped")
		}
		wakeups <- struct{}{}
		cancel()
		return context.Canceled
	}, 0)

	require.Equal(t, 2, calls)
	select {
	case <-wakeups:
	default:
		t.Fatal("expected wakeup after reconnect")
	}
}

package common

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestWaitForDeployment_ImmediateSuccess(t *testing.T) {
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return &DeploymentRecord{Status: "deployed"}, nil
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	})
	require.NoError(t, err)
}

func TestWaitForDeployment_TransitionsToDeployed(t *testing.T) {
	var calls atomic.Int32
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		switch calls.Add(1) {
		case 1, 2:
			return &DeploymentRecord{Status: "deploying"}, nil
		default:
			return &DeploymentRecord{Status: "deployed"}, nil
		}
	}
	var progress []string
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
		Progress: func(status string, _ time.Duration) {
			progress = append(progress, status)
		},
	})
	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "expected three polls before reaching deployed")
	assert.Equal(t, []string{"deploying", "deploying"}, progress,
		"progress callback fires once per non-terminal poll")
}

func TestWaitForDeployment_TerminalFailureMismatch(t *testing.T) {
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return &DeploymentRecord{Status: "failed", Error: "image pull backoff"}, nil
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), `reached state "failed"`)
	assert.Contains(t, err.Error(), `image pull backoff`,
		"the deployment's error message is surfaced in the wait failure")
}

func TestWaitForDeployment_WaitingForFailedSucceedsOnFailed(t *testing.T) {
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return &DeploymentRecord{Status: "failed", Error: "this is expected"}, nil
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		TargetStatus: "failed",
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	})
	require.NoError(t, err, "explicit --for=failed treats failed as the success state")
}

func TestWaitForDeployment_NotFoundWhenWaitingForDeployed(t *testing.T) {
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return nil, database.ErrNotFound
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestWaitForDeployment_TargetDeletedSuccess(t *testing.T) {
	var calls atomic.Int32
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		switch calls.Add(1) {
		case 1:
			return &DeploymentRecord{Status: "terminating"}, nil
		default:
			return nil, client.ErrNotFound
		}
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		TargetDeleted: true,
		Timeout:       time.Second,
		PollInterval:  time.Millisecond,
	})
	require.NoError(t, err)
	assert.Equal(t, int32(2), calls.Load())
}

func TestWaitForDeployment_Timeout(t *testing.T) {
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return &DeploymentRecord{Status: "deploying"}, nil
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      50 * time.Millisecond,
		PollInterval: 5 * time.Millisecond,
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "timed out")
	assert.Contains(t, err.Error(), `current status: deploying`)
}

func TestWaitForDeployment_OneShotPollsOnceThenExits(t *testing.T) {
	var calls atomic.Int32
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		calls.Add(1)
		return &DeploymentRecord{Status: "deploying"}, nil
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      0,
		PollInterval: time.Millisecond,
	})
	require.Error(t, err, "Timeout=0 should poll once and return the timeout error if not yet at target")
	assert.Contains(t, err.Error(), "timed out")
	assert.Equal(t, int32(1), calls.Load(), "Timeout=0 must not loop")
}

func TestWaitForDeployment_ContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var calls atomic.Int32
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		if calls.Add(1) >= 2 {
			cancel()
		}
		return &DeploymentRecord{Status: "deploying"}, nil
	}
	err := WaitForDeployment(ctx, resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: 5 * time.Millisecond,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestWaitForDeployment_ResolveErrorIsPropagated(t *testing.T) {
	sentinel := errors.New("registry exploded")
	resolve := func(ctx context.Context) (*DeploymentRecord, error) {
		return nil, sentinel
	}
	err := WaitForDeployment(context.Background(), resolve, WaitOptions{
		Timeout:      time.Second,
		PollInterval: time.Millisecond,
	})
	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
}

func TestWaitForDeployment_NilResolverIsRejected(t *testing.T) {
	err := WaitForDeployment(context.Background(), nil, WaitOptions{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolve function is nil")
}

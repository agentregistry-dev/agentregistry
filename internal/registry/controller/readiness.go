package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// defaultReadinessRequeueInterval is how long the controller waits before
// re-probing a Deployment whose runtime workload has not converged yet.
const defaultReadinessRequeueInterval = 5 * time.Second

// requeueAfterError asks processQueueItem to requeue a key after a fixed delay
// without treating the reconcile as a failure.
type requeueAfterError struct {
	after   time.Duration
	message string
}

func (e *requeueAfterError) Error() string {
	return fmt.Sprintf("requeue after %s: %s", e.after, e.message)
}

func asRequeueAfter(err error) (*requeueAfterError, bool) {
	var requeue *requeueAfterError
	if errors.As(err, &requeue) {
		return requeue, true
	}
	return nil, false
}

func (c *DeploymentController) readinessRequeueInterval() time.Duration {
	if c.ReadinessRequeueInterval > 0 {
		return c.ReadinessRequeueInterval
	}
	return defaultReadinessRequeueInterval
}

// reconcileReadiness polls adapters that implement DeploymentReadinessProber
// and persists their conditions. Adapters without the capability keep the
// conditions their Apply returned, and outcome/message pass through unchanged.
func (c *DeploymentController) reconcileReadiness(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	adapter types.DeploymentAdapter,
	input types.ApplyInput,
	outcome, message string,
) (string, string, error) {
	prober, ok := adapter.(types.DeploymentReadinessProber)
	if !ok {
		return outcome, message, nil
	}
	result, err := prober.ProbeReadiness(ctx, input)
	if err != nil {
		return "", "", fmt.Errorf("adapter %q readiness probe: %w", adapter.Type(), err)
	}
	if result == nil {
		return outcome, message, nil
	}
	if conditionsChanged(deployment.Status, result.Conditions) {
		if err := c.persistReadinessResult(ctx, deployment, result); err != nil {
			return "", "", err
		}
	}
	if !result.Ready {
		reason := "workload not ready"
		for _, cond := range result.Conditions {
			if cond.Type == "Ready" && cond.Message != "" {
				reason = cond.Message
			}
		}
		return "", "", &requeueAfterError{after: c.readinessRequeueInterval(), message: reason}
	}
	return outcome, message, nil
}

// persistReadinessResult writes status only. Status-only writes do not emit
// control-plane events, so polling cannot wake the controller in a loop.
func (c *DeploymentController) persistReadinessResult(
	ctx context.Context,
	deployment *v1alpha1.Deployment,
	result *types.ReadinessResult,
) error {
	err := c.deploymentStore().PatchStatus(
		ctx,
		deployment.Metadata.NamespaceOrDefault(),
		deployment.Metadata.Name,
		"",
		v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
			for _, cond := range result.Conditions {
				s.SetCondition(cond)
			}
		}),
	)
	if err != nil {
		return fmt.Errorf("persist readiness result: %w", err)
	}
	return nil
}

// conditionsChanged reports whether applying want to current would alter any
// condition's observable fields. LastTransitionTime is ignored because
// SetCondition preserves it when Status is unchanged.
func conditionsChanged(current v1alpha1.Status, want []v1alpha1.Condition) bool {
	for _, cond := range want {
		existing := current.GetCondition(cond.Type)
		if existing == nil ||
			existing.Status != cond.Status ||
			existing.Reason != cond.Reason ||
			existing.Message != cond.Message ||
			existing.ObservedGeneration != cond.ObservedGeneration {
			return true
		}
	}
	return false
}

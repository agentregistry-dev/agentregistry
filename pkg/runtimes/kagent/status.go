package kagent

import (
	"errors"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func translationFailure(
	target v1alpha1.Object,
	err error,
	now time.Time,
) (*types.ApplyResult, error) {
	switch {
	case errors.Is(err, ErrDependencyNotReady):
		return nil, err
	case errors.Is(err, errInvalidDependency):
		return FailedApplyResult(target, "InvalidDependency", err.Error(), now), nil
	case errors.Is(err, errUnsupported):
		return FailedApplyResult(target, "UnsupportedTarget", err.Error(), now), nil
	default:
		return nil, err
	}
}

func successfulApplyResult(
	runtimeID string,
	runtimeNamespace string,
	extraCondition *v1alpha1.Condition,
	now time.Time,
) (*types.ApplyResult, error) {
	conditions := completedConditions(now)
	if extraCondition != nil {
		conditions = append(conditions, *extraCondition)
	}
	runtime := &v1alpha1.DeploymentRuntimeStatus{ID: runtimeID, Name: runtimeID}
	if extraCondition != nil {
		runtime.Endpoint = extraCondition.Message
	}
	return &types.ApplyResult{
		Conditions: conditions,
		InternalMeta: &types.DeploymentInternalMeta{
			RuntimeID:        runtimeID,
			RuntimeName:      runtimeID,
			RuntimeNamespace: runtimeNamespace,
		},
		Runtime: runtime,
	}, nil
}

// FailedApplyResult builds the Ready=False/Failed result the adapter persists
// for a rejected apply; MCPServer targets also get MCPServerURL=False.
func FailedApplyResult(
	target v1alpha1.Object,
	reason, message string,
	now time.Time,
) *types.ApplyResult {
	conditions := failedConditions(message, now)
	if _, ok := target.(*v1alpha1.MCPServer); ok {
		conditions = append(conditions, v1alpha1.Condition{
			Type:               mcpServerURLCondition,
			Status:             v1alpha1.ConditionFalse,
			Reason:             reason,
			Message:            message,
			LastTransitionTime: now,
		})
	}
	return &types.ApplyResult{Conditions: conditions}
}

func deploymentRuntimeID(deployment *types.DeploymentRecord) string {
	if deployment == nil {
		return ""
	}
	return deployment.InternalMeta.RuntimeID
}

func deploymentRuntimeNamespace(deployment *types.DeploymentRecord) string {
	if deployment == nil {
		return ""
	}
	return deployment.InternalMeta.RuntimeNamespace
}

func completedConditions(now time.Time) []v1alpha1.Condition {
	return []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Completed",
			Message:            "Deployment completed",
			LastTransitionTime: now,
		},
	}
}

func failedConditions(message string, now time.Time) []v1alpha1.Condition {
	return []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Failed",
			Message:            message,
			LastTransitionTime: now,
		},
	}
}

func removedConditions(now time.Time) []v1alpha1.Condition {
	return []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Removed",
			Message:            "Kagent workload removed",
			LastTransitionTime: now,
		},
	}
}

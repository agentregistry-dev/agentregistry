package kagent

import (
	"encoding/json"
	"errors"
	"fmt"
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
	metadata map[string]string,
	extraCondition *v1alpha1.Condition,
	now time.Time,
) (*types.ApplyResult, error) {
	conditions := completedConditions(now)
	if extraCondition != nil {
		conditions = append(conditions, *extraCondition)
	}
	result := &types.ApplyResult{Conditions: conditions}
	if remoteID := metadata[metaRemoteID]; remoteID != "" {
		result.RuntimeMetadata = map[string]string{
			runtimeRemoteIDAnnotationKey: remoteID,
		}
	}
	if err := attachRuntimeMetadata(result, metadata); err != nil {
		return nil, err
	}
	return result, nil
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

func attachRuntimeMetadata(result *types.ApplyResult, metadata map[string]string) error {
	encoded, err := json.Marshal(metadata)
	if err != nil {
		return fmt.Errorf("encode kagent runtime metadata: %w", err)
	}
	result.Details = map[string]json.RawMessage{runtimeDetailsKey: encoded}
	return nil
}

func deploymentRuntimeMetadata(deployment *v1alpha1.Deployment) (map[string]string, error) {
	if deployment == nil {
		return nil, nil
	}
	var metadata map[string]string
	found, err := deployment.Status.GetDetailsKey(runtimeDetailsKey, &metadata)
	if err != nil {
		return nil, fmt.Errorf("decode kagent runtime metadata: %w", err)
	}
	if !found {
		return nil, nil
	}
	return metadata, nil
}

func completedConditions(now time.Time) []v1alpha1.Condition {
	return []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Completed",
			Message:            "deployment completed",
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
			Message:            "kagent workload removed",
			LastTransitionTime: now,
		},
	}
}

package kagent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestCompletedConditions(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	assert.Equal(t, []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionTrue,
			Reason:             "Completed",
			Message:            "deployment completed",
			LastTransitionTime: now,
		},
	}, completedConditions(now))
}

func TestFailedConditions(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	got := failedConditions("harness agents are not supported", now)
	assert.Equal(t, []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Failed",
			Message:            "harness agents are not supported",
			LastTransitionTime: now,
		},
	}, got)
}

func TestRemovedConditions(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	got := removedConditions(now)
	assert.Equal(t, []v1alpha1.Condition{
		{
			Type:               "Ready",
			Status:             v1alpha1.ConditionFalse,
			Reason:             "Removed",
			Message:            "kagent workload removed",
			LastTransitionTime: now,
		},
	}, got)
}

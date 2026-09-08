package kagent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestLifecycleConditions(t *testing.T) {
	now := time.Date(2026, 8, 24, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name        string
		conditions  []v1alpha1.Condition
		wantStatus  v1alpha1.ConditionStatus
		wantReason  string
		wantMessage string
	}{
		{
			name:        "completed",
			conditions:  completedConditions(now),
			wantStatus:  v1alpha1.ConditionTrue,
			wantReason:  "Completed",
			wantMessage: "Deployment completed",
		},
		{
			name:        "failed",
			conditions:  failedConditions("harness agents are not supported", now),
			wantStatus:  v1alpha1.ConditionFalse,
			wantReason:  "Failed",
			wantMessage: "harness agents are not supported",
		},
		{
			name:        "removed",
			conditions:  removedConditions(now),
			wantStatus:  v1alpha1.ConditionFalse,
			wantReason:  "Removed",
			wantMessage: "Kagent workload removed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Len(t, tt.conditions, 1)
			condition := tt.conditions[0]
			assert.Equal(t, "Ready", condition.Type)
			assert.Equal(t, tt.wantStatus, condition.Status)
			assert.Equal(t, tt.wantReason, condition.Reason)
			assert.Equal(t, tt.wantMessage, condition.Message)
			assert.Equal(t, now, condition.LastTransitionTime)
		})
	}
}

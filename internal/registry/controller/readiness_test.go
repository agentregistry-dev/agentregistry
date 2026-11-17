package controller

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestConditionsChanged(t *testing.T) {
	current := v1alpha1.Status{Conditions: []v1alpha1.Condition{{
		Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "WorkloadAvailable",
		Message: "ok", ObservedGeneration: 2, LastTransitionTime: time.Unix(10, 0),
	}}}
	same := v1alpha1.Condition{
		Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "WorkloadAvailable",
		Message: "ok", ObservedGeneration: 2, LastTransitionTime: time.Unix(99, 0),
	}
	if conditionsChanged(current, []v1alpha1.Condition{same}) {
		t.Fatal("identical condition with a new timestamp must not count as a change")
	}
	for name, mutate := range map[string]func(*v1alpha1.Condition){
		"status":     func(c *v1alpha1.Condition) { c.Status = v1alpha1.ConditionFalse },
		"reason":     func(c *v1alpha1.Condition) { c.Reason = "WaitingForWorkload" },
		"message":    func(c *v1alpha1.Condition) { c.Message = "0 of 1 available" },
		"generation": func(c *v1alpha1.Condition) { c.ObservedGeneration = 3 },
		"new type":   func(c *v1alpha1.Condition) { c.Type = "Progressing" },
	} {
		changed := same
		mutate(&changed)
		if !conditionsChanged(current, []v1alpha1.Condition{changed}) {
			t.Errorf("%s change not detected", name)
		}
	}
}

func TestRequeueAfterErrorUnwraps(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", &requeueAfterError{after: time.Second, message: "waiting"})
	requeue, ok := asRequeueAfter(err)
	if !ok || requeue.after != time.Second {
		t.Fatalf("asRequeueAfter(%v) = %v, %v", err, requeue, ok)
	}
	if _, ok := asRequeueAfter(fmt.Errorf("plain")); ok {
		t.Fatal("plain errors must not be treated as requeue requests")
	}
}

func TestReadinessRequeueIntervalDefault(t *testing.T) {
	c := &DeploymentController{}
	if got := c.readinessRequeueInterval(); got != defaultReadinessRequeueInterval {
		t.Fatalf("default interval = %s", got)
	}
	c.ReadinessRequeueInterval = time.Millisecond
	if got := c.readinessRequeueInterval(); got != time.Millisecond {
		t.Fatalf("override interval = %s", got)
	}
}

func TestDeploymentControllerStatusPatchClearsStaleReadyCondition(t *testing.T) {
	deployment := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Generation: 3},
	}
	current, err := json.Marshal(v1alpha1.Status{Conditions: []v1alpha1.Condition{{
		Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "WorkloadAvailable",
	}, {
		Type: "Progressing", Status: v1alpha1.ConditionFalse, Reason: "RolloutComplete",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	patch := deploymentControllerStatusPatch(deployment, &types.ApplyResult{
		Conditions: []v1alpha1.Condition{{
			Type: "Progressing", Status: v1alpha1.ConditionTrue, Reason: "Applied",
		}},
	}, "", "", nil)
	updated, err := patch(current)
	if err != nil {
		t.Fatal(err)
	}
	var status v1alpha1.Status
	if err := json.Unmarshal(updated, &status); err != nil {
		t.Fatal(err)
	}
	if status.GetCondition("Ready") != nil {
		t.Fatal("stale Ready condition must be cleared while a new rollout is progressing")
	}
	if !status.IsConditionTrue("Progressing") {
		t.Fatal("Progressing=True condition must be preserved")
	}
}

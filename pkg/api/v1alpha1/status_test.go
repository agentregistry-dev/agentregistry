package v1alpha1

import (
	"testing"
	"time"
)

func TestStatus_SetCondition_AppendsWhenAbsent(t *testing.T) {
	s := &Status{}
	s.SetCondition(Condition{Type: "Ready", Status: ConditionTrue, Reason: "Healthy"})

	if len(s.Conditions) != 1 {
		t.Fatalf("expected 1 condition, got %d", len(s.Conditions))
	}
	c := s.Conditions[0]
	if c.Type != "Ready" || c.Status != ConditionTrue || c.Reason != "Healthy" {
		t.Fatalf("unexpected condition: %+v", c)
	}
	if c.LastTransitionTime.IsZero() {
		t.Fatal("expected LastTransitionTime to be set on append")
	}
}

func TestStatus_SetCondition_PreservesTimestampOnSameStatus(t *testing.T) {
	s := &Status{}
	s.SetCondition(Condition{Type: "Ready", Status: ConditionTrue, Reason: "First"})
	original := s.Conditions[0].LastTransitionTime

	time.Sleep(2 * time.Millisecond)
	s.SetCondition(Condition{Type: "Ready", Status: ConditionTrue, Reason: "Second"})

	if len(s.Conditions) != 1 {
		t.Fatalf("expected 1 condition after update, got %d", len(s.Conditions))
	}
	if !s.Conditions[0].LastTransitionTime.Equal(original) {
		t.Fatal("LastTransitionTime must not flip when Status is unchanged")
	}
	if s.Conditions[0].Reason != "Second" {
		t.Fatalf("Reason should be overwritten; got %q", s.Conditions[0].Reason)
	}
}

func TestStatus_SetCondition_UpdatesTimestampOnStatusFlip(t *testing.T) {
	s := &Status{}
	s.SetCondition(Condition{Type: "Ready", Status: ConditionTrue})
	original := s.Conditions[0].LastTransitionTime

	time.Sleep(2 * time.Millisecond)
	s.SetCondition(Condition{Type: "Ready", Status: ConditionFalse, Reason: "Crashing"})

	if s.Conditions[0].LastTransitionTime.Equal(original) {
		t.Fatal("LastTransitionTime must flip when Status changes")
	}
	if s.Conditions[0].Status != ConditionFalse {
		t.Fatalf("Status should be ConditionFalse; got %q", s.Conditions[0].Status)
	}
}

func TestStatus_GetCondition(t *testing.T) {
	s := &Status{
		Conditions: []Condition{
			{Type: "Ready", Status: ConditionTrue},
			{Type: "Validated", Status: ConditionFalse},
		},
	}
	if c := s.GetCondition("Ready"); c == nil || c.Status != ConditionTrue {
		t.Fatal("Ready lookup failed")
	}
	if c := s.GetCondition("Missing"); c != nil {
		t.Fatal("expected nil for unknown condition")
	}
}

func TestStatus_IsConditionTrue(t *testing.T) {
	s := &Status{
		Conditions: []Condition{
			{Type: "Ready", Status: ConditionTrue},
			{Type: "Degraded", Status: ConditionFalse},
		},
	}
	if !s.IsConditionTrue("Ready") {
		t.Fatal("Ready should be true")
	}
	if s.IsConditionTrue("Degraded") {
		t.Fatal("Degraded should not be true")
	}
	if s.IsConditionTrue("Missing") {
		t.Fatal("missing condition should not be true")
	}
}

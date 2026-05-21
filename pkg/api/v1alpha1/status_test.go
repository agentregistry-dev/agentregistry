package v1alpha1

import (
	"encoding/json"
	"strings"
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

func TestStatus_ConditionsRoundTrip(t *testing.T) {
	s := Status{
		ObservedGeneration: 7,
		Conditions: []Condition{{
			Type:               "Synced",
			Status:             ConditionTrue,
			ObservedGeneration: 7,
		}},
	}
	data, err := MarshalStatusForStorage(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Status
	if err := UnmarshalStatusFromStorage(data, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Conditions) != 1 {
		t.Errorf("Conditions not round-tripped: got %d, want 1", len(got.Conditions))
	}
	if got.ObservedGeneration != 7 {
		t.Errorf("ObservedGeneration not round-tripped: got %d, want 7", got.ObservedGeneration)
	}
	if got.Conditions[0].ObservedGeneration != 7 {
		t.Errorf("Condition.ObservedGeneration not round-tripped: got %d, want 7", got.Conditions[0].ObservedGeneration)
	}
	wire, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) == "" || strings.Contains(string(wire), "observedGeneration") {
		t.Fatalf("observedGeneration must stay hidden from wire JSON: %s", string(wire))
	}
}

func TestStatus_SetDetailsKey_MergesMultipleKeys(t *testing.T) {
	s := &Status{}
	if err := s.SetDetailsKey("agentgateway", map[string]any{"listeners": 2}); err != nil {
		t.Fatalf("first SetDetailsKey: %v", err)
	}
	if err := s.SetDetailsKey("kubernetes", map[string]any{"replicas": 3}); err != nil {
		t.Fatalf("second SetDetailsKey: %v", err)
	}

	var gw struct {
		Listeners int `json:"listeners"`
	}
	ok, err := s.GetDetailsKey("agentgateway", &gw)
	if err != nil || !ok {
		t.Fatalf("GetDetailsKey(agentgateway): ok=%v err=%v", ok, err)
	}
	if gw.Listeners != 2 {
		t.Errorf("agentgateway.listeners: got %d, want 2", gw.Listeners)
	}

	var k8s struct {
		Replicas int `json:"replicas"`
	}
	ok, err = s.GetDetailsKey("kubernetes", &k8s)
	if err != nil || !ok {
		t.Fatalf("GetDetailsKey(kubernetes): ok=%v err=%v", ok, err)
	}
	if k8s.Replicas != 3 {
		t.Errorf("kubernetes.replicas: got %d, want 3", k8s.Replicas)
	}
}

func TestStatus_SetDetailsKey_DeletesWithNil(t *testing.T) {
	s := &Status{}
	if err := s.SetDetailsKey("a", map[string]int{"x": 1}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDetailsKey("b", map[string]int{"y": 2}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDetailsKey("a", nil); err != nil {
		t.Fatalf("delete via nil: %v", err)
	}
	if ok, _ := s.GetDetailsKey("a", &map[string]int{}); ok {
		t.Error("a should have been deleted")
	}
	if ok, _ := s.GetDetailsKey("b", &map[string]int{}); !ok {
		t.Error("b should be preserved")
	}

	// Removing the last key should null out Details entirely.
	if err := s.SetDetailsKey("b", nil); err != nil {
		t.Fatal(err)
	}
	if s.Details != nil {
		t.Errorf("Details should be nil after last key removed; got %s", string(s.Details))
	}
}

func TestStatus_SetDetailsKeyJSON_RejectsInvalidJSON(t *testing.T) {
	s := &Status{}
	err := s.SetDetailsKeyJSON("bad", json.RawMessage(`{not json`))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	if !strings.Contains(err.Error(), "not valid JSON") {
		t.Errorf("error should mention invalid JSON; got: %v", err)
	}
	if s.Details != nil {
		t.Errorf("Details should remain unset on invalid input; got %s", string(s.Details))
	}
}

func TestStatus_SetDetailsKey_RejectsEmptyKey(t *testing.T) {
	s := &Status{}
	if err := s.SetDetailsKey("", 1); err == nil {
		t.Error("SetDetailsKey(\"\") should error")
	}
	if err := s.SetDetailsKeyJSON("", json.RawMessage(`1`)); err == nil {
		t.Error("SetDetailsKeyJSON(\"\") should error")
	}
}

func TestStatus_DetailsRoundTrip(t *testing.T) {
	s := Status{ObservedGeneration: 3}
	if err := s.SetDetailsKey("agentgateway", map[string]any{"listeners": 2}); err != nil {
		t.Fatal(err)
	}
	data, err := MarshalStatusForStorage(s)
	if err != nil {
		t.Fatal(err)
	}
	var got Status
	if err := UnmarshalStatusFromStorage(data, &got); err != nil {
		t.Fatal(err)
	}
	var payload struct {
		Listeners int `json:"listeners"`
	}
	ok, err := got.GetDetailsKey("agentgateway", &payload)
	if err != nil || !ok {
		t.Fatalf("Details not round-tripped: ok=%v err=%v", ok, err)
	}
	if payload.Listeners != 2 {
		t.Errorf("agentgateway.listeners: got %d, want 2", payload.Listeners)
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

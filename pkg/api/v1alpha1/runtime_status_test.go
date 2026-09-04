package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestRuntimeStatusStorageRoundTrip(t *testing.T) {
	runtime := Runtime{}
	if err := runtime.UnmarshalStatus(json.RawMessage(`{"observedGeneration":3,"conditions":[{"type":"Ready","status":"True"}]}`)); err != nil {
		t.Fatalf("UnmarshalStatus: %v", err)
	}

	if runtime.Status.ObservedGeneration != 3 {
		t.Fatalf("ObservedGeneration = %d, want 3", runtime.Status.ObservedGeneration)
	}
	if condition := runtime.Status.GetCondition("Ready"); condition == nil || condition.Status != ConditionTrue {
		t.Fatalf("Ready condition = %#v", condition)
	}
	raw, err := runtime.MarshalStatus()
	if err != nil {
		t.Fatalf("MarshalStatus: %v", err)
	}
	if string(raw) != `{"observedGeneration":3,"conditions":[{"type":"Ready","status":"True"}]}` {
		t.Fatalf("MarshalStatus = %s", raw)
	}
}

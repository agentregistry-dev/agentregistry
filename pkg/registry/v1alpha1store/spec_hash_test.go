package v1alpha1store

import (
	"encoding/json"
	"testing"
)

func TestSpecHash_OrderInvariant(t *testing.T) {
	a := json.RawMessage(`{"foo":1,"bar":2}`)
	b := json.RawMessage(`{"bar":2,"foo":1}`)
	if SpecHash(a) != SpecHash(b) {
		t.Errorf("hash should be order-invariant; got a=%s b=%s", SpecHash(a), SpecHash(b))
	}
}

func TestSpecHash_WhitespaceInvariant(t *testing.T) {
	a := json.RawMessage(`{"foo":1}`)
	b := json.RawMessage(`{ "foo" : 1 }`)
	if SpecHash(a) != SpecHash(b) {
		t.Errorf("hash should be whitespace-invariant")
	}
}

func TestSpecHash_ContentSensitive(t *testing.T) {
	a := json.RawMessage(`{"foo":1}`)
	b := json.RawMessage(`{"foo":2}`)
	if SpecHash(a) == SpecHash(b) {
		t.Errorf("hash should differ on content change")
	}
}

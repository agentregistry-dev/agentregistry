package v1alpha1

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestTypeMeta_MarshalsInline(t *testing.T) {
	a := Agent{
		TypeMeta: TypeMeta{APIVersion: GroupVersion, Kind: KindAgent},
		Metadata: ObjectMeta{Name: "x"},
	}
	out, err := json.Marshal(a)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"apiVersion":"ar.dev/v1alpha1"`) {
		t.Fatalf("apiVersion not inlined at top level: %s", s)
	}
	if !strings.Contains(s, `"kind":"Agent"`) {
		t.Fatalf("kind not inlined at top level: %s", s)
	}
	if strings.Contains(s, `"TypeMeta"`) {
		t.Fatalf("TypeMeta leaked as nested key: %s", s)
	}
}

func TestObjectMeta_OmitsZeroTimes(t *testing.T) {
	m := ObjectMeta{Name: "x"}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "createdAt") || strings.Contains(s, "updatedAt") {
		t.Fatalf("zero timestamps should be omitted: %s", s)
	}
}

func TestObjectMeta_EmitsNonZeroTimes(t *testing.T) {
	m := ObjectMeta{Name: "x", CreatedAt: time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC)}
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "createdAt") {
		t.Fatalf("non-zero createdAt should serialize: %s", string(out))
	}
}

func TestRawObject_DecodesSpecAsRawJSON(t *testing.T) {
	doc := []byte(`{
  "apiVersion": "ar.dev/v1alpha1",
  "kind": "Agent",
  "metadata": { "name": "x" },
  "spec": { "title": "hi", "image": "img" }
}`)
	var raw RawObject
	if err := json.Unmarshal(doc, &raw); err != nil {
		t.Fatal(err)
	}
	if raw.Kind != KindAgent || raw.Metadata.Name != "x" {
		t.Fatalf("envelope: %+v", raw)
	}
	// Spec should be raw JSON — decodable into the typed shape.
	var spec AgentSpec
	if err := json.Unmarshal(raw.Spec, &spec); err != nil {
		t.Fatalf("spec unmarshal: %v", err)
	}
	if spec.Title != "hi" || spec.Image != "img" {
		t.Fatalf("spec mismatch: %+v", spec)
	}
}

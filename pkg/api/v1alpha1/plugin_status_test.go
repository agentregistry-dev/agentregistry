package v1alpha1

import (
	"encoding/json"
	"testing"
)

func TestPluginStatusRoundTrip(t *testing.T) {
	in := &Plugin{}
	in.Status.ObservedGeneration = 5
	in.Status.SetCondition(Condition{Type: "Ready", Status: ConditionTrue, Reason: "Resolved"})
	in.Status.ResolvedSource = &PluginResolvedSource{Type: PluginOriginTypeGit, Commit: "abc123"}
	in.Status.Manifest = &PluginManifest{Name: "deploy", Version: "1.2.0"}
	in.Status.Inventory = &PluginInventory{Skills: []PluginSkill{{Name: "deploy", Description: "Deploys"}}}

	raw, err := in.MarshalStatus()
	if err != nil {
		t.Fatalf("MarshalStatus: %v", err)
	}

	out := &Plugin{}
	if err := out.UnmarshalStatus(raw); err != nil {
		t.Fatalf("UnmarshalStatus: %v", err)
	}

	if out.Status.ObservedGeneration != 5 {
		t.Errorf("observedGeneration = %d, want 5", out.Status.ObservedGeneration)
	}
	if !out.Status.IsConditionTrue("Ready") {
		t.Error("Ready condition did not round-trip")
	}
	if out.Status.ResolvedSource == nil || out.Status.ResolvedSource.Commit != "abc123" || out.Status.ResolvedSource.Type != PluginOriginTypeGit {
		t.Errorf("resolvedSource did not round-trip: %+v", out.Status.ResolvedSource)
	}
	if out.Status.Manifest == nil || out.Status.Manifest.Name != "deploy" || out.Status.Manifest.Version != "1.2.0" {
		t.Errorf("manifest did not round-trip: %+v", out.Status.Manifest)
	}
	if out.Status.Inventory == nil || len(out.Status.Inventory.Skills) != 1 || out.Status.Inventory.Skills[0].Name != "deploy" {
		t.Errorf("inventory did not round-trip: %+v", out.Status.Inventory)
	}
}

// TestPluginStatusOmitsNilCustomFields guards the patch-skip byte-stability
// contract: absent server-determined fields must not emit stray JSON keys.
func TestPluginStatusOmitsNilCustomFields(t *testing.T) {
	p := &Plugin{}
	p.Status.SetCondition(Condition{Type: "Ready", Status: ConditionFalse, Reason: "Progressing"})

	raw, err := p.MarshalStatus()
	if err != nil {
		t.Fatalf("MarshalStatus: %v", err)
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, k := range []string{"resolvedSource", "manifest", "inventory"} {
		if _, ok := m[k]; ok {
			t.Errorf("nil %q must be omitted, got key in %s", k, string(raw))
		}
	}
}

package main

import "testing"

func TestGeneratedSpecSchemasReplaceReflectedSchemas(t *testing.T) {
	spec := generateSpec("test")
	if err := applyGeneratedSpecSchemas(spec); err != nil {
		t.Fatal(err)
	}
	agent := spec.Components.Schemas.Map()["AgentSpec"]
	if agent == nil || agent.Properties["compatibleHarnesses"] == nil {
		t.Fatal("AgentSpec was not replaced with its generated schema")
	}
	harness := agent.Properties["compatibleHarnesses"].Items
	if harness == nil || len(harness.Required) != 1 || harness.Required[0] != "type" {
		t.Fatalf("generated required fields = %#v", harness)
	}
}

package database

import "testing"

func TestNewSchema(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"simple", "agentregistry", false},
		{"underscore", "agentregistry_enterprise", false},
		{"leading underscore", "_internal", false},
		{"empty", "", true},
		{"leading digit", "1schema", true},
		{"dash", "agent-registry", true},
		{"dot", "a.b", true},
		{"quote injection", `a"; DROP`, true},
		{"space", "two words", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSchema(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NewSchema(%q) err=%v, wantErr=%v", tt.input, err, tt.wantErr)
			}
		})
	}
}

func TestSchemaAccessors(t *testing.T) {
	s, err := NewSchema("agentregistry")
	if err != nil {
		t.Fatalf("NewSchema: %v", err)
	}
	if got := s.Name(); got != "agentregistry" {
		t.Errorf("Name() = %q, want agentregistry", got)
	}
	if got, want := s.Quoted(), `"agentregistry"`; got != want {
		t.Errorf("Quoted() = %q, want %q", got, want)
	}
	if got, want := s.Qualify("agents"), `"agentregistry"."agents"`; got != want {
		t.Errorf("Qualify() = %q, want %q", got, want)
	}
}

func TestSchemaRegistry(t *testing.T) {
	r := NewSchemaRegistry()

	if _, ok := r.Get("oss"); ok {
		t.Fatal("Get on empty registry should report absent")
	}

	if err := r.Add("oss", "agentregistry"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	sch, ok := r.Get("oss")
	if !ok {
		t.Fatal("Get after Add should find the source")
	}
	if got, want := sch.Qualify("agents"), `"agentregistry"."agents"`; got != want {
		t.Errorf("Qualify() = %q, want %q", got, want)
	}

	if err := r.Add("oss", "other"); err == nil {
		t.Error("Add of duplicate source should error")
	}
	if err := r.Add("bad", "1nope"); err == nil {
		t.Error("Add of invalid schema name should error")
	}
}

func TestSchemaRegistryMustGet(t *testing.T) {
	r := NewSchemaRegistry()
	if err := r.Add("oss", "agentregistry"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := r.MustGet("oss").Name(); got != "agentregistry" {
		t.Errorf("MustGet().Name() = %q, want agentregistry", got)
	}
	defer func() {
		if recover() == nil {
			t.Error("MustGet on absent source should panic")
		}
	}()
	_ = r.MustGet("missing")
}

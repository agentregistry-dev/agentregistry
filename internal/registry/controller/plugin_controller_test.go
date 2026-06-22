package controller

import (
	"errors"
	"fmt"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/bundle"
	"github.com/agentregistry-dev/agentregistry/internal/registry/plugins/source"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestClassifyResolveErr(t *testing.T) {
	tests := []struct {
		name         string
		err          error
		wantReason   string
		wantTerminal bool
	}{
		{"unsupported origin", fmt.Errorf("wrap: %w", source.ErrUnsupportedOrigin), "OriginUnsupported", true},
		{"invalid bundle", fmt.Errorf("wrap: %w", bundle.ErrInvalidBundle), "SourceInvalid", true},
		{"transient", errors.New("dial tcp: timeout"), "OriginUnresolvable", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reason, terminal := classifyResolveErr(tt.err)
			if reason != tt.wantReason || terminal != tt.wantTerminal {
				t.Fatalf("classifyResolveErr = (%q, %v), want (%q, %v)", reason, terminal, tt.wantReason, tt.wantTerminal)
			}
		})
	}
}

func TestPluginReconciled(t *testing.T) {
	plugin := func(observed, gen int64, ready v1alpha1.ConditionStatus) *v1alpha1.Plugin {
		p := &v1alpha1.Plugin{}
		p.Metadata.Generation = gen
		p.Status.ObservedGeneration = observed
		p.Status.SetCondition(v1alpha1.Condition{Type: pluginReadyCondition, Status: ready, Reason: "x"})
		return p
	}

	// Gates on ObservedGeneration only; Ready true/false is irrelevant.
	if !pluginReconciled(plugin(3, 3, v1alpha1.ConditionTrue)) {
		t.Fatal("observed==gen (success) should be reconciled")
	}
	if !pluginReconciled(plugin(3, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed==gen (terminal failure) should be reconciled — must NOT re-resolve every tick")
	}
	if pluginReconciled(plugin(2, 3, v1alpha1.ConditionFalse)) {
		t.Fatal("observed<gen (retryable / pending) should NOT be reconciled")
	}
	if pluginReconciled(&v1alpha1.Plugin{}) {
		t.Fatal("a fresh plugin (generation 0 in this zero value) should NOT be reconciled")
	}
}

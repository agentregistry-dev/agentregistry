package service

import "testing"

func TestDiscoveredDeploymentID_Deterministic(t *testing.T) {
	first := discoveredDeploymentID("mcpserver", "default", "io.github.acme/weather")
	second := discoveredDeploymentID("mcpserver", "default", "io.github.acme/weather")
	if first == "" {
		t.Fatal("expected non-empty discovered deployment id")
	}
	if first != second {
		t.Fatalf("expected deterministic discovered deployment id, got %q and %q", first, second)
	}
}

func TestDiscoveredDeploymentID_VariesByNamespaceAndKind(t *testing.T) {
	base := discoveredDeploymentID("mcpserver", "default", "io.github.acme/weather")
	otherNamespace := discoveredDeploymentID("mcpserver", "prod", "io.github.acme/weather")
	otherKind := discoveredDeploymentID("agent", "default", "io.github.acme/weather")
	if base == otherNamespace {
		t.Fatalf("expected namespace-specific id; got %q for both", base)
	}
	if base == otherKind {
		t.Fatalf("expected kind-specific id; got %q for both", base)
	}
}

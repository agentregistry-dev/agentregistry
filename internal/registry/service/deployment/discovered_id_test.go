package deployment_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
)

func TestDiscoveredDeploymentID_Deterministic(t *testing.T) {
	first := deployment.DiscoveredDeploymentID("kubernetes-default", "mcp", "io.github.acme/weather", "unknown")
	second := deployment.DiscoveredDeploymentID("kubernetes-default", "mcp", "io.github.acme/weather", "unknown")
	if first == "" {
		t.Fatal("expected non-empty discovered deployment id")
	}
	if first != second {
		t.Fatalf("expected deterministic discovered deployment id, got %q and %q", first, second)
	}
}

func TestDiscoveredDeploymentID_VariesByProviderAndResourceType(t *testing.T) {
	base := deployment.DiscoveredDeploymentID("kubernetes-default", "mcp", "io.github.acme/weather", "unknown")
	otherProvider := deployment.DiscoveredDeploymentID("aws-main", "mcp", "io.github.acme/weather", "unknown")
	otherResourceType := deployment.DiscoveredDeploymentID("kubernetes-default", "agent", "io.github.acme/weather", "unknown")
	if base == otherProvider {
		t.Fatalf("expected provider-specific id; got %q for both", base)
	}
	if base == otherResourceType {
		t.Fatalf("expected resource-type-specific id; got %q for both", base)
	}
}

func TestDiscoveredDeploymentID_VariesByNamespace(t *testing.T) {
	first := deployment.DiscoveredDeploymentIDWithNamespace("kubernetes-default", "mcp", "io.github.acme/weather", "unknown", "team-a")
	second := deployment.DiscoveredDeploymentIDWithNamespace("kubernetes-default", "mcp", "io.github.acme/weather", "unknown", "team-b")
	if first == second {
		t.Fatalf("expected namespace-specific id; got %q for both", first)
	}
}

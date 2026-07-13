package gateway

import (
	"context"

	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// Target identifies the gateway instance that rendered config should be
// applied to or removed from. It is distinct from the pre-existing
// types.Target (Address/Hostname); the two coexist across the package
// boundary with no conflict.
type Target struct {
	Name string
}

// Engine renders a desired Config into the native *types.AgentGatewayConfig
// and manages its lifecycle against a Target. Render is a pure translation
// with no side effects; Apply applies already-rendered config to a target;
// Remove removes previously-applied config. Callers depend on this
// interface, not on any concrete renderer.
type Engine interface {
	Render(ctx context.Context, desired Config) (*types.AgentGatewayConfig, error)
	Apply(ctx context.Context, target Target, rendered *types.AgentGatewayConfig) error
	Remove(ctx context.Context, target Target) error
}

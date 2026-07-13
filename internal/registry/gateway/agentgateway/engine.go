// Package agentgateway is the agentgateway-specific implementation of the
// gateway package's rendering and lifecycle contracts.
//
// Engine translates a gateway.Config into the repo's native
// *types.AgentGatewayConfig and applies it to a gateway.Target; Applier is
// the injected side-effecting half of that contract.
package agentgateway

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/internal/registry/gateway"
	types "github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/types"
)

// Engine renders a desired gateway.Config into the native
// *types.AgentGatewayConfig and manages its lifecycle against a
// gateway.Target. Render is a pure translation with no side effects; Apply
// applies already-rendered config to a target; Remove removes previously-
// applied config. Callers depend on this interface, not on any concrete
// renderer.
type Engine interface {
	Render(ctx context.Context, desired gateway.Config) (*types.AgentGatewayConfig, error)
	Apply(ctx context.Context, target gateway.Target, rendered *types.AgentGatewayConfig) error
	Remove(ctx context.Context, target gateway.Target) error
}

// Applier applies or removes rendered native gateway config against a
// target. It is injected into AgentGatewayEngine so that Render stays a
// pure, testable translation and all side effects are delegated to the
// caller-provided implementation.
type Applier interface {
	Apply(ctx context.Context, target gateway.Target, cfg *types.AgentGatewayConfig) error
	Remove(ctx context.Context, target gateway.Target) error
}

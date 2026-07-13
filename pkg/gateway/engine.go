package gateway

import "context"

// Target identifies the gateway instance that config should be applied to or
// removed from, keyed by deployment id (Target.Name).
type Target struct {
	Name string
}

// Engine applies a desired, gateway-agnostic Config to a Target and removes
// previously-applied config. It is deliberately implementation-agnostic:
// callers depend only on this interface and the generic Config, never on any
// concrete engine's native config format. Concrete engines (e.g. agentgateway)
// translate Config into their native representation internally during Apply.
type Engine interface {
	Apply(ctx context.Context, target Target, desired Config) error
	Remove(ctx context.Context, target Target) error
}

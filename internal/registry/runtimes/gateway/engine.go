// Package gateway defines a gateway-implementation-agnostic contract for
// modeling, rendering, and applying gateway configuration.
//
// The desired configuration (Config and its supporting types) describes
// gateway concepts — listeners, routes, backends, and policies — without
// leaking any implementation-native shapes. A ConfigEngine translates that
// desired model into an opaque RenderedConfig and applies it to a Target.
package gateway

import "context"

// Target identifies the gateway instance that rendered config should be
// applied to or removed from. It is distinct from the pre-existing
// types.Target (Address/Hostname); the two coexist across the package
// boundary with no conflict.
type Target struct {
	Name string
	UID  string
}

// RenderedConfig is an opaque wrapper hiding implementation-native config
// from generic callers. The unexported marker method ensures it can only be
// constructed and inspected within this package, so callers may hold and pass
// a RenderedConfig but cannot read or build the native shape.
type RenderedConfig interface {
	// isRenderedConfig is an unexported marker so callers outside this package
	// cannot construct or inspect native rendered config.
	isRenderedConfig()
}

// ConfigEngine renders a desired Config into an opaque RenderedConfig and
// manages its lifecycle against a Target. Render is a pure translation with
// no side effects; Apply applies already-rendered config to a target; Remove
// removes previously-applied config. Callers depend on this interface, not on
// any concrete renderer.
type ConfigEngine interface {
	Render(ctx context.Context, desired Config) (RenderedConfig, error)
	Apply(ctx context.Context, target Target, rendered RenderedConfig) error
	Remove(ctx context.Context, target Target) error
}

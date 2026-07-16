package gateway

import "context"

// Target identifies the gateway config scope.
type Target struct {
	Name       string
	Attributes map[string]string
}

// Engine applies and removes gateway-agnostic desired config.
type Engine interface {
	Apply(ctx context.Context, target Target, desired Config) error
	Remove(ctx context.Context, target Target) error
}

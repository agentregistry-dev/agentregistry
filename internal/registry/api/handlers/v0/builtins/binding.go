package builtins

import (
	"fmt"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
)

// Binding is the per-kind wiring contract owned by each kind's
// registration file (agent.go, mcp_server.go, ...). Wire is invoked by
// RegisterBuiltins with the per-call resource.Config (already populated
// with Store + per-kind hooks); each binding closes over its concrete
// generic type T so the resource.Register[T] / RegisterReadme[T]
// instantiations resolve at compile time.
//
// Adding a new built-in kind is a single new file under this package
// with an init() that calls Register — no central switch to update.
type Binding struct {
	Kind string
	Wire func(api huma.API, cfg resource.Config)
}

var bindings = map[string]Binding{}

// Register adds a Binding to the package-level table. Panics if Kind is
// empty or already registered — both are init()-time programming errors.
func Register(b Binding) {
	if b.Kind == "" {
		panic("builtins.Register: Kind is required")
	}
	if b.Wire == nil {
		panic(fmt.Sprintf("builtins.Register: Wire is required for kind %q", b.Kind))
	}
	if _, dup := bindings[b.Kind]; dup {
		panic(fmt.Sprintf("builtins.Register: kind %q already registered", b.Kind))
	}
	bindings[b.Kind] = b
}

// lookup returns the binding for the given kind, or false if unregistered.
// Unexported because consumers should iterate v1alpha1.BuiltinKinds and
// call this by kind name — the iteration order is controlled by the
// canonical BuiltinKinds slice, not the map.
func lookup(kind string) (Binding, bool) {
	b, ok := bindings[kind]
	return b, ok
}

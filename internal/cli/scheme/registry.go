// Package scheme is the CLI dispatch layer over v1alpha1: it owns the
// alias-flexible Kind lookup table (so `arctl get mcp` and `arctl get
// mcpserver` both resolve), per-kind table-render metadata, and the
// per-kind cobra→apiClient closures (`Get`, `List`, `Delete`,
// `ToYAMLFunc`). YAML decode itself flows through pkg/api/v1alpha1.Scheme
// — this package holds only CLI-specific concerns.
//
// Kinds are registered at package init by the declarative package. The
// table is global, populated once, and never mutated afterwards (no
// SetRegistry hook — there's no test or enterprise build that needs to
// swap it). Aliases collide → panic at boot.
package scheme

import (
	"context"
	"errors"
	"fmt"
)

type Column struct {
	Header string
}

type ListFunc func(context.Context) ([]any, error)
type RowFunc func(any) []string
type ToYAMLFunc func(any) any
type GetFunc func(ctx context.Context, name, version string) (any, error)

// DeleteFunc deletes a single (name, version) of the kind. force=true
// asks the server to skip its PostDelete reconciliation hook (e.g.
// provider teardown for Deployment); kinds that don't honor force
// should ignore the flag. The CLI's `arctl delete --force` plumbs
// through here.
type DeleteFunc func(ctx context.Context, name, version string, force bool) error

type Kind struct {
	Kind       string
	Plural     string
	Aliases    []string
	ListFunc   ListFunc
	RowFunc    RowFunc
	ToYAMLFunc ToYAMLFunc
	Get        GetFunc
	Delete     DeleteFunc

	TableColumns []Column
}

var (
	kindsByAlias = map[string]*Kind{}
	kindsOrdered []*Kind
)

// Register adds a Kind to the global lookup table. Panics if any of
// Kind / Plural / Aliases collides with an already-registered entry —
// callers are expected to register at package init, where a panic is
// the right fail-fast behavior.
func Register(k *Kind) {
	if k == nil || k.Kind == "" {
		panic("scheme.Register: kind is required")
	}

	names := make([]string, 0, 2+len(k.Aliases))
	names = append(names, k.Kind)
	if k.Plural != "" {
		names = append(names, k.Plural)
	}
	names = append(names, k.Aliases...)

	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		key := kindAliasKey(name)
		if key == "" {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		if _, exists := kindsByAlias[key]; exists {
			panic(fmt.Sprintf("scheme.Register: %q already registered", name))
		}
		seen[key] = struct{}{}
	}

	for name := range seen {
		kindsByAlias[name] = k
	}
	kindsOrdered = append(kindsOrdered, k)
}

// ErrUnknownKind is returned by Lookup when no Kind is registered
// under the given name or alias.
var ErrUnknownKind = errors.New("unknown kind")

// Lookup resolves a user-typed name (canonical Kind, plural, or alias —
// case-insensitive) to its registered *Kind, or ErrUnknownKind.
func Lookup(name string) (*Kind, error) {
	if k, ok := kindsByAlias[kindAliasKey(name)]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownKind, name)
}

// All returns every registered Kind in registration order.
func All() []*Kind {
	out := make([]*Kind, len(kindsOrdered))
	copy(out, kindsOrdered)
	return out
}

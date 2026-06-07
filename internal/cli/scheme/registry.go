// Package scheme is the CLI dispatch layer over v1alpha1: it owns the
// alias-flexible Kind lookup table (so `arctl get mcp` and `arctl get
// mcpserver` both resolve), per-kind table-render metadata, and the
// per-kind cobra→registry-client callbacks (`Get`, `List`, `Delete`,
// `ToYAMLFunc`). YAML decode itself flows through pkg/api/v1alpha1.Scheme
// — this package holds only CLI-specific concerns.
//
// Kinds are registered at package init by the declarative package. The
// table is global, populated once, and never mutated afterwards (no
// SetRegistry hook — there is no current caller that needs to swap it).
// Aliases collide → panic at boot.
package scheme

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

type Column struct {
	Header string
}

// ListOpts are CLI-facing filters forwarded to the per-kind ListFunc.
// Empty fields mean "no filter" — the default `arctl get <plural>` lists
// every row of the kind.
type ListOpts struct {
	// Tag, when set, restricts the list to rows with this tag value
	// (tagged content kinds only). Mutually exclusive with LatestOnly.
	Tag string
	// LatestOnly restricts the list to the literal "latest" tag (tagged
	// content kinds) or the latest mutable-object row.
	LatestOnly bool
}

type ListFunc func(context.Context, *client.Client, ListOpts) ([]any, error)
type RowFunc func(any) []string
type ToYAMLFunc func(any) any
type GetFunc func(ctx context.Context, c *client.Client, name, tag string) (any, error)

// DeleteFunc deletes a single (name, tag) of the kind.
type DeleteFunc func(ctx context.Context, c *client.Client, name, tag string) error

// ListTagsFunc returns every live tag row for a single (name).
// Set only on taggable artifact kinds (Agent, MCPServer, Skill, etc.).
// Nil for kinds whose identity is not tagged (Deployment, Runtime) —
// callers must check for nil and reject `--all-tags` cleanly.
type ListTagsFunc func(ctx context.Context, c *client.Client, name string) ([]any, error)

// DeleteAllTagsFunc soft-deletes every live tag of a single (name) in one
// server round-trip. Set only on taggable artifact kinds. Nil for kinds whose
// identity is not tagged.
type DeleteAllTagsFunc func(ctx context.Context, c *client.Client, name string) error

type Kind struct {
	Kind          string
	Plural        string
	Aliases       []string
	ListFunc      ListFunc
	RowFunc       RowFunc
	ToYAMLFunc    ToYAMLFunc
	Get           GetFunc
	Delete        DeleteFunc
	ListTags      ListTagsFunc
	DeleteAllTags DeleteAllTagsFunc

	TableColumns []Column
}

var (
	kindsByAlias = map[string]*Kind{}
	kindsOrdered []*Kind
)

// Registry is an isolated Kind lookup table.
type Registry struct {
	kindsByAlias map[string]*Kind
	kindsOrdered []*Kind
}

// NewRegistry returns an isolated registry populated with the given kinds.
func NewRegistry(kinds ...*Kind) *Registry {
	r := &Registry{
		kindsByAlias: map[string]*Kind{},
		kindsOrdered: []*Kind{},
	}
	for _, k := range kinds {
		r.Register(k)
	}
	return r
}

// Register adds a Kind to the global lookup table. Panics if any of
// Kind / Plural / Aliases collides with an already-registered entry —
// callers are expected to register at package init, where a panic is
// the right fail-fast behavior.
func Register(k *Kind) {
	r := defaultRegistry()
	r.Register(k)
	kindsByAlias = r.kindsByAlias
	kindsOrdered = r.kindsOrdered
}

func defaultRegistry() *Registry {
	return &Registry{
		kindsByAlias: kindsByAlias,
		kindsOrdered: kindsOrdered,
	}
}

// Register adds a Kind to the registry. Panics if any of Kind / Plural /
// Aliases collides with an already-registered entry.
func (r *Registry) Register(k *Kind) {
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
		if _, exists := r.kindsByAlias[key]; exists {
			panic(fmt.Sprintf("scheme.Register: %q already registered", name))
		}
		seen[key] = struct{}{}
	}

	for name := range seen {
		r.kindsByAlias[name] = k
	}
	r.kindsOrdered = append(r.kindsOrdered, k)
}

// ErrUnknownKind is returned by Lookup when no Kind is registered
// under the given name or alias.
var ErrUnknownKind = errors.New("unknown kind")

// Lookup resolves a user-typed name (canonical Kind, plural, or alias —
// case-insensitive) to its registered *Kind, or ErrUnknownKind.
func Lookup(name string) (*Kind, error) {
	return defaultRegistry().Lookup(name)
}

// Lookup resolves a user-typed name in this registry.
func (r *Registry) Lookup(name string) (*Kind, error) {
	if k, ok := r.kindsByAlias[kindAliasKey(name)]; ok {
		return k, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownKind, name)
}

// All returns every registered Kind in registration order.
func All() []*Kind {
	return defaultRegistry().All()
}

// All returns every Kind in registration order.
func (r *Registry) All() []*Kind {
	out := make([]*Kind, len(r.kindsOrdered))
	copy(out, r.kindsOrdered)
	return out
}

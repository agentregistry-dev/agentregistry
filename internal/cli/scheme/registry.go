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
type DeleteFunc func(ctx context.Context, name, version string) error

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

type Registry struct {
	byName  map[string]*Kind
	ordered []*Kind
}

func NewRegistry() *Registry {
	return &Registry{byName: make(map[string]*Kind)}
}

func (r *Registry) Register(k Kind) {
	if k.Kind == "" {
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
		if _, exists := r.byName[key]; exists {
			panic(fmt.Sprintf("scheme.Register: %q already registered", name))
		}
		seen[key] = struct{}{}
	}

	copyKind := k
	for name := range seen {
		r.byName[name] = &copyKind
	}
	r.ordered = append(r.ordered, &copyKind)
}

var ErrUnknownKind = errors.New("unknown kind")

func (r *Registry) Lookup(name string) (*Kind, error) {
	if r == nil {
		return nil, fmt.Errorf("%w %q", ErrUnknownKind, name)
	}
	if kind, ok := r.byName[kindAliasKey(name)]; ok {
		return kind, nil
	}
	return nil, fmt.Errorf("%w %q", ErrUnknownKind, name)
}

func (r *Registry) All() []*Kind {
	if r == nil {
		return nil
	}
	out := make([]*Kind, len(r.ordered))
	copy(out, r.ordered)
	return out
}

package v1alpha1store

import (
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// NewStores builds one *Store per registered v1alpha1 Kind, bound to the table
// declared by v1alpha1.KindDescriptor. The returned map is keyed by Kind name
// (e.g. "Agent", "MCPServer") and is the single input the router/apply layers
// take. They never look up tables by string literal themselves.
//
// The variadic opts are applied to every Store produced. Downstream
// callers pass WithAuditor(...) here to plumb a single audit sink
// across all kinds in one call.
func NewStores(pool *pgxpool.Pool, opts ...StoreOption) map[string]*Store {
	descriptors := v1alpha1.KindDescriptors()
	out := make(map[string]*Store, len(descriptors))
	for _, descriptor := range descriptors {
		store, err := newStoreForDescriptor(pool, descriptor, opts...)
		if err != nil {
			panic(err)
		}
		out[descriptor.Kind] = store
	}
	return out
}

// NewStoreForKind builds the store described by the registered v1alpha1 kind.
// Use this when a caller needs a dedicated handle for one kind but still wants
// table and storage behavior to come from the shared kind registry.
func NewStoreForKind(pool *pgxpool.Pool, kind string, opts ...StoreOption) (*Store, error) {
	descriptor, ok := v1alpha1.KindDescriptorFor(kind)
	if !ok {
		return nil, fmt.Errorf("v1alpha1store: kind %q is not registered", kind)
	}
	return newStoreForDescriptor(pool, descriptor, opts...)
}

func newStoreForDescriptor(pool *pgxpool.Pool, descriptor v1alpha1.KindDescriptor, opts ...StoreOption) (*Store, error) {
	kind := descriptor.Kind
	if descriptor.Table == "" {
		return nil, fmt.Errorf("v1alpha1store: no table registered for kind %s", kind)
	}
	// Prepend WithKind so per-kind audit events name the kind correctly even if
	// the inbound object's TypeMeta is empty. Caller-supplied opts win.
	kindOpts := append([]StoreOption{WithKind(kind)}, opts...)
	switch descriptor.Storage {
	case v1alpha1.KindStorageMutableObject:
		return NewMutableObjectStore(pool, descriptor.Table, kindOpts...), nil
	case v1alpha1.KindStorageTaggedArtifact:
		return NewStore(pool, descriptor.Table, kindOpts...), nil
	default:
		return nil, fmt.Errorf("v1alpha1store: no storage behavior registered for kind %s", kind)
	}
}

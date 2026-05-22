package v1alpha1store

import (
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
		kind := descriptor.Kind
		if descriptor.Table == "" {
			panic("v1alpha1store: no table registered for kind " + kind)
		}
		// Prepend WithKind so per-kind audit events name the kind
		// correctly even if the inbound object's TypeMeta is empty.
		// Caller-supplied opts win (they appear after WithKind in the
		// option chain).
		kindOpts := append([]StoreOption{WithKind(kind)}, opts...)
		switch descriptor.Storage {
		case v1alpha1.KindStorageMutableObject:
			out[kind] = NewMutableObjectStore(pool, descriptor.Table, kindOpts...)
		case v1alpha1.KindStorageTaggedArtifact:
			out[kind] = NewStore(pool, descriptor.Table, kindOpts...)
		default:
			panic("v1alpha1store: no storage behavior registered for kind " + kind)
		}
	}
	return out
}

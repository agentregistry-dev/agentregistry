package v1alpha1store

import (
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// builtInKinds is the OSS store allowlist. Table names and storage behavior
// come from v1alpha1.KindDescriptor so the registration record remains the
// single source of per-kind metadata.
var builtInKinds = map[string]struct{}{
	v1alpha1.KindAgent:      {},
	v1alpha1.KindMCPServer:  {},
	v1alpha1.KindSkill:      {},
	v1alpha1.KindPlugin:     {},
	v1alpha1.KindPrompt:     {},
	v1alpha1.KindRuntime:    {},
	v1alpha1.KindDeployment: {},
}

// NewStores builds one *Store per OSS built-in v1alpha1 Kind, bound to its
// canonical table. The returned map is keyed by Kind name (e.g. "Agent",
// "MCPServer") and is the single input the router/apply layers take. They
// never look up tables by string literal themselves.
//
// Kinds whose descriptors use KindStorageMutableObject are bound through
// NewMutableObjectStore. Every other built-in kind uses NewStore
// (tagged-artifact behavior). Extension kinds are intentionally not built here;
// the composition root wires them from V1Alpha1StoreTables after this function
// returns.
//
// The variadic opts are applied to every Store produced. Downstream
// callers pass WithAuditor(...) here to plumb a single audit sink
// across all kinds in one call.
func NewStores(pool *pgxpool.Pool, schemas *pkgdb.SchemaRegistry, opts ...StoreOption) map[string]*Store {
	// The OSS source's schema is statically known to be registered by the
	// composition root before stores are built; a missing entry is a
	// wiring bug, so MustGet panics rather than returning a nil schema
	// that would surface as a malformed query later.
	ossSchema := schemas.MustGet(pkgdb.OSSSourceName)
	out := make(map[string]*Store, len(builtInKinds))
	for _, descriptor := range v1alpha1.KindDescriptors() {
		kind := descriptor.Kind
		if _, ok := builtInKinds[kind]; !ok {
			continue
		}
		table := storeTableNameFromDescriptor(descriptor)
		if table == "" {
			panic("v1alpha1store: empty table for built-in kind " + kind)
		}
		// Prepend WithKind so per-kind audit events name the kind
		// correctly even if the inbound object's TypeMeta is empty.
		// Caller-supplied opts win (they appear after WithKind in the
		// option chain).
		kindOpts := append([]StoreOption{WithKind(kind)}, opts...)
		if descriptor.Storage == v1alpha1.KindStorageMutableObject {
			out[kind] = NewMutableObjectStore(pool, ossSchema, table, kindOpts...)
			continue
		}
		out[kind] = NewStore(pool, ossSchema, table, kindOpts...)
	}
	for kind := range builtInKinds {
		if _, ok := out[kind]; !ok {
			panic("v1alpha1store: no kind descriptor registered for built-in kind " + kind)
		}
	}
	return out
}

func storeTableNameFromDescriptor(descriptor v1alpha1.KindDescriptor) string {
	table := strings.TrimSpace(descriptor.Table)
	return strings.TrimPrefix(table, "v1alpha1.")
}

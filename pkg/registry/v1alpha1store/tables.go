package v1alpha1store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// TableFor is the canonical mapping from v1alpha1 Kind name to its
// backing table. The names are unqualified here; NewStores qualifies
// each with the OSS schema (from the injected registry) so queries name
// the schema explicitly rather than relying on the connection's
// search_path.
//
// Downstream builds that register additional kinds via
// v1alpha1.Scheme.Register should extend their own copy of this map
// rather than mutating this one; the OSS side treats it as effectively
// const after init.
var TableFor = map[string]string{
	v1alpha1.KindAgent:      "agents",
	v1alpha1.KindMCPServer:  "mcp_servers",
	v1alpha1.KindSkill:      "skills",
	v1alpha1.KindPrompt:     "prompts",
	v1alpha1.KindRuntime:    "runtimes",
	v1alpha1.KindDeployment: "deployments",
}

// NewStores builds one *Store per built-in v1alpha1 Kind, bound
// to its canonical table. The returned map is keyed by Kind name (e.g.
// "Agent", "MCPServer") and is the single input the router/apply
// layers take. They never look up tables by string literal themselves.
//
// KindDeployment and KindRuntime are bound through NewMutableObjectStore —
// both are infra/lifecycle state, not tagged artifacts. Every other built-in
// kind uses NewStore (tagged-artifact behavior). Iterates v1alpha1.BuiltinKinds so
// registration order stays stable across builds (important for
// OpenAPI output).
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
	out := make(map[string]*Store, len(v1alpha1.BuiltinKinds))
	for _, kind := range v1alpha1.BuiltinKinds {
		table, ok := TableFor[kind]
		if !ok {
			// BuiltinKinds and TableFor must stay in sync — a missing
			// table here is a coding error, not a runtime condition.
			panic("v1alpha1store: no table registered for kind " + kind)
		}
		// Prepend WithKind so per-kind audit events name the kind
		// correctly even if the inbound object's TypeMeta is empty.
		// Caller-supplied opts win (they appear after WithKind in the
		// option chain).
		kindOpts := append([]StoreOption{WithKind(kind)}, opts...)
		if kind == v1alpha1.KindRuntime {
			out[kind] = NewMutableObjectStore(pool, ossSchema, table, kindOpts...)
			continue
		}
		if kind == v1alpha1.KindDeployment {
			out[kind] = NewMutableObjectStore(pool, ossSchema, table, kindOpts...)
			continue
		}
		out[kind] = NewStore(pool, ossSchema, table, kindOpts...)
	}
	return out
}

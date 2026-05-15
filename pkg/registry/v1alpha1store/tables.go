package v1alpha1store

import (
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// TableFor is the canonical mapping from v1alpha1 Kind name to
// its backing table in the dedicated `v1alpha1.*` PostgreSQL schema.
// Callers that need a *Store should prefer NewStores below
// rather than constructing one per kind.
//
// Downstream builds that register additional kinds via
// v1alpha1.Scheme.Register should extend their own copy of this map
// rather than mutating this one; the OSS side treats it as effectively
// const after init.
var TableFor = map[string]string{
	v1alpha1.KindAgent:      "v1alpha1.agents",
	v1alpha1.KindMCPServer:  "v1alpha1.mcp_servers",
	v1alpha1.KindSkill:      "v1alpha1.skills",
	v1alpha1.KindPrompt:     "v1alpha1.prompts",
	v1alpha1.KindRuntime:    "v1alpha1.runtimes",
	v1alpha1.KindDeployment: "v1alpha1.deployments",
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
func NewStores(pool *pgxpool.Pool, opts ...StoreOption) map[string]*Store {
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
			out[kind] = NewMutableObjectStore(pool, table, kindOpts...)
			continue
		}
		if kind == v1alpha1.KindDeployment {
			out[kind] = NewMutableObjectStore(pool, table, kindOpts...)
			continue
		}
		out[kind] = NewStore(pool, table, kindOpts...)
	}
	return out
}

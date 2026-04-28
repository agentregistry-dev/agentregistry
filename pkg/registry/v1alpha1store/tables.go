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
// Enterprise builds that register additional kinds via
// v1alpha1.Scheme.Register should extend their own copy of this map
// rather than mutating this one; the OSS side treats it as effectively
// const after init.
var TableFor = map[string]string{
	v1alpha1.KindAgent:      "v1alpha1.agents",
	v1alpha1.KindMCPServer:  "v1alpha1.mcp_servers",
	v1alpha1.KindSkill:      "v1alpha1.skills",
	v1alpha1.KindPrompt:     "v1alpha1.prompts",
	v1alpha1.KindProvider:   "v1alpha1.providers",
	v1alpha1.KindDeployment: "v1alpha1.deployments",
}

// NewStores builds one *Store per built-in v1alpha1 Kind, bound
// to its canonical table. The returned map is keyed by Kind name (e.g.
// "Agent", "MCPServer") and is the single input the router/apply/
// importer layers take — they never look up tables by string literal
// themselves.
//
// Iterates v1alpha1.BuiltinKinds so registration order stays stable
// across builds (important for OpenAPI output).
func NewStores(pool *pgxpool.Pool) map[string]*Store {
	out := make(map[string]*Store, len(v1alpha1.BuiltinKinds))
	for _, kind := range v1alpha1.BuiltinKinds {
		table, ok := TableFor[kind]
		if !ok {
			// BuiltinKinds and TableFor must stay in sync — a missing
			// table here is a coding error, not a runtime condition.
			panic("v1alpha1store: no table registered for kind " + kind)
		}
		out[kind] = NewStore(pool, table)
	}
	return out
}

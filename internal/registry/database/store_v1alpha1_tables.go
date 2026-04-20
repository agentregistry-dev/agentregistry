package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// V1Alpha1TableFor is the canonical mapping from v1alpha1 Kind name to
// its backing table in the dedicated `v1alpha1.*` PostgreSQL schema.
// Callers that need a *Store should prefer NewV1Alpha1Stores below
// rather than constructing one per kind.
//
// Enterprise builds that register additional kinds via
// v1alpha1.Scheme.Register should extend their own copy of this map
// rather than mutating this one; the OSS side treats it as effectively
// const after init.
var V1Alpha1TableFor = map[string]string{
	v1alpha1.KindAgent:      "v1alpha1.agents",
	v1alpha1.KindMCPServer:  "v1alpha1.mcp_servers",
	v1alpha1.KindSkill:      "v1alpha1.skills",
	v1alpha1.KindPrompt:     "v1alpha1.prompts",
	v1alpha1.KindProvider:   "v1alpha1.providers",
	v1alpha1.KindDeployment: "v1alpha1.deployments",
}

// NewV1Alpha1Stores builds one *Store per built-in v1alpha1 Kind,
// bound to its canonical table. The returned map is keyed by Kind
// name (e.g. "Agent", "MCPServer") and is the single input the
// router/apply/importer layers take — they never look up tables by
// string literal themselves.
//
// Iterates v1alpha1.BuiltinKinds so registration order stays stable
// across builds (important for OpenAPI output).
func NewV1Alpha1Stores(pool *pgxpool.Pool) map[string]*Store {
	out := make(map[string]*Store, len(v1alpha1.BuiltinKinds))
	for _, kind := range v1alpha1.BuiltinKinds {
		table, ok := V1Alpha1TableFor[kind]
		if !ok {
			// Impossible unless BuiltinKinds and V1Alpha1TableFor drift
			// out of sync — guarded by a compile-time-ish test rather
			// than a panic here.
			continue
		}
		out[kind] = NewStore(pool, table)
	}
	return out
}

// NewV1Alpha1Resolver returns a v1alpha1.ResolverFunc that dispatches
// cross-kind ResourceRef existence checks against the supplied
// Stores map. Consumers: the router wires one into its apply
// handler; the Importer consumes one during per-object ResolveRefs.
//
// Dangling references return v1alpha1.ErrDanglingRef so callers can
// distinguish "row missing" from "database unavailable"; unknown
// kinds return wrapped v1alpha1.ErrInvalidRef.
func NewV1Alpha1Resolver(stores map[string]*Store) v1alpha1.ResolverFunc {
	return func(ctx context.Context, ref v1alpha1.ResourceRef) error {
		store, ok := stores[ref.Kind]
		if !ok {
			return fmt.Errorf("%w: unknown kind %q", v1alpha1.ErrInvalidRef, ref.Kind)
		}
		var err error
		if ref.Version == "" {
			_, err = store.GetLatest(ctx, ref.Namespace, ref.Name)
		} else {
			_, err = store.Get(ctx, ref.Namespace, ref.Name, ref.Version)
		}
		if err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return v1alpha1.ErrDanglingRef
			}
			return err
		}
		return nil
	}
}

// NewV1Alpha1UniqueRemoteURLsChecker returns a v1alpha1.UniqueRemoteURLsFunc
// that scans the kind's table via Store.FindReferrers with JSONB
// containment fragment `{"remotes":[{"url":"<url>"}]}`. Kinds that don't
// carry Remotes (Prompt, Provider, Deployment) never reach this — their
// (Object).ValidateUniqueRemoteURLs methods return nil without calling
// the checker.
//
// Search scope is cross-namespace: the URL is a global real-world
// identifier, not a namespace-scoped one. The namespace parameter on the
// UniqueRemoteURLsFunc signature is advisory and ignored here.
//
// On conflict, returns a plain error naming the conflicting (kind, name)
// so the surrounding FieldError path captures the remote index.
func NewV1Alpha1UniqueRemoteURLsChecker(stores map[string]*Store) v1alpha1.UniqueRemoteURLsFunc {
	return func(ctx context.Context, kind, _ /* namespace */, url, excludeName string) error {
		store, ok := stores[kind]
		if !ok {
			return fmt.Errorf("unique-remote-urls: unknown kind %q", kind)
		}
		fragment, err := json.Marshal(map[string]any{
			"remotes": []map[string]string{{"url": url}},
		})
		if err != nil {
			return fmt.Errorf("unique-remote-urls: encode fragment: %w", err)
		}
		refs, err := store.FindReferrers(ctx, "", fragment, false)
		if err != nil {
			return fmt.Errorf("unique-remote-urls: scan: %w", err)
		}
		for _, r := range refs {
			if r.Metadata.Name == excludeName {
				continue
			}
			return fmt.Errorf("remote url %s is already used by %s %q", url, kind, r.Metadata.Name)
		}
		return nil
	}
}

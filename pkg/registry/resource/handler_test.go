//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
)

// registerAgent wires the generic resource handler for *v1alpha1.Agent onto
// the given Huma API, against the supplied Store. It's a test-local helper
// so we don't pull the full registry_app into these tests.
func registerAgent(api huma.API, store *v1alpha1store.Store) {
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
}

// newTestPool is defined in database/store_v1alpha1_testutil.go. Each test
// gets its own isolated DB.
func TestResourceRegister_AgentCRUD(t *testing.T) {
	t.Helper()

	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// PUT a new agent in the default namespace.
	putBody := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: "default",
			Name:      "alice",
			Version:   "v1.0.0",
			Labels:    map[string]string{"team": "platform"},
		},
		Spec: v1alpha1.AgentSpec{
			Title: "Alice",
			Image: "ghcr.io/example/alice:1.0.0",
		},
	}
	resp := api.Put("/v0/namespaces/default/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var gotAgent v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, v1alpha1.GroupVersion, gotAgent.APIVersion)
	require.Equal(t, v1alpha1.KindAgent, gotAgent.Kind)
	require.Equal(t, "default", gotAgent.Metadata.Namespace)
	require.Equal(t, "alice", gotAgent.Metadata.Name)
	require.Equal(t, "v1.0.0", gotAgent.Metadata.Version)
	require.EqualValues(t, 1, gotAgent.Metadata.Generation)
	require.Equal(t, "Alice", gotAgent.Spec.Title)
	require.Equal(t, "platform", gotAgent.Metadata.Labels["team"])

	// GET exact version.
	resp = api.Get("/v0/namespaces/default/agents/alice/v1.0.0")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "alice", gotAgent.Metadata.Name)

	// GET latest.
	resp = api.Get("/v0/namespaces/default/agents/alice")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "v1.0.0", gotAgent.Metadata.Version)

	// LIST in namespace with label selector.
	resp = api.Get("/v0/namespaces/default/agents?labels=team%3Dplatform")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items      []v1alpha1.Agent `json:"items"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "alice", list.Items[0].Metadata.Name)

	// LIST across all namespaces — also finds the row.
	resp = api.Get("/v0/agents?labels=team%3Dplatform")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)

	// Re-apply with same spec — generation must stay at 1.
	resp = api.Put("/v0/namespaces/default/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.EqualValues(t, 1, gotAgent.Metadata.Generation, "no-op apply preserves generation")

	// PUT with mutated spec — generation bumps to 2.
	putBody.Spec.Title = "Alice v2"
	resp = api.Put("/v0/namespaces/default/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.EqualValues(t, 2, gotAgent.Metadata.Generation)
	require.Equal(t, "Alice v2", gotAgent.Spec.Title)

	// DELETE — soft-delete: 204, but the row remains with a deletionTimestamp.
	resp = api.Delete("/v0/namespaces/default/agents/alice/v1.0.0")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// Default GET excludes terminating rows; GetLatest returns 404.
	resp = api.Get("/v0/namespaces/default/agents/alice")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())

	// GET on the exact version still finds the terminating row (soft-delete).
	resp = api.Get("/v0/namespaces/default/agents/alice/v1.0.0")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.NotNil(t, gotAgent.Metadata.DeletionTimestamp)

	// Default list hides terminating rows.
	resp = api.Get("/v0/namespaces/default/agents")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Empty(t, list.Items)

	// includeTerminating=true surfaces them.
	resp = api.Get("/v0/namespaces/default/agents?includeTerminating=true")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
}

func TestResourceRegister_AgentNamespaceIsolation(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Same name in two different namespaces — no conflict.
	bodyTeamA := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "shared", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "A's"},
	}
	bodyTeamB := bodyTeamA
	bodyTeamB.Metadata.Namespace = "team-b"
	bodyTeamB.Spec.Title = "B's"

	resp := api.Put("/v0/namespaces/team-a/agents/shared/v1", bodyTeamA)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	resp = api.Put("/v0/namespaces/team-b/agents/shared/v1", bodyTeamB)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Namespaced GETs resolve the right one.
	var got v1alpha1.Agent
	resp = api.Get("/v0/namespaces/team-a/agents/shared/v1")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "A's", got.Spec.Title)

	resp = api.Get("/v0/namespaces/team-b/agents/shared/v1")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "B's", got.Spec.Title)

	// Cross-namespace list returns both.
	resp = api.Get("/v0/agents")
	require.Equal(t, http.StatusOK, resp.Code)
	var list struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)

	// Namespaced list returns one.
	resp = api.Get("/v0/namespaces/team-a/agents")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "team-a", list.Items[0].Metadata.Namespace)
}

func TestResourceRegister_AgentListCursorPagination(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	for _, name := range []string{"one", "two", "three"} {
		body := v1alpha1.Agent{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Version: "v1"},
			Spec:     v1alpha1.AgentSpec{Title: name},
		}
		resp := api.Put(fmt.Sprintf("/v0/namespaces/default/agents/%s/v1", url.PathEscape(name)), body)
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	}

	var page struct {
		Items      []v1alpha1.Agent `json:"items"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}

	resp := api.Get("/v0/agents?limit=2")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page))
	require.Len(t, page.Items, 2)
	require.NotEmpty(t, page.NextCursor)

	resp = api.Get("/v0/agents?limit=2&cursor=" + url.QueryEscape(page.NextCursor))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var page2 struct {
		Items      []v1alpha1.Agent `json:"items"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &page2))
	require.Len(t, page2.Items, 1)
	require.Empty(t, page2.NextCursor)

	seen := map[string]bool{}
	for _, item := range append(page.Items, page2.Items...) {
		require.False(t, seen[item.Metadata.Name], "cursor pagination should not repeat rows")
		seen[item.Metadata.Name] = true
	}
	require.Len(t, seen, 3)
}

func TestResourceRegister_AgentListRejectsInvalidCursor(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	resp := api.Get("/v0/agents?cursor=not-a-valid-cursor")
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), "invalid cursor")
}

func TestResourceRegister_AgentWrongKindRejected(t *testing.T) {
	t.Helper()

	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Body carries Kind: "Skill" but PUT targets the agents handler.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: "Skill"},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "bob", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "wrong kind"},
	}
	resp := api.Put("/v0/namespaces/default/agents/bob/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestResourceRegister_AgentPathMismatchRejected(t *testing.T) {
	t.Helper()
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "mismatched", Version: "v1"},
	}
	resp := api.Put("/v0/namespaces/default/agents/alice/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, fmt.Sprintf("body=%s", resp.Body.String()))
}

func TestResourceRegister_ValidationRejectsBadVersion(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "bad", Version: "latest"},
		Spec:     v1alpha1.AgentSpec{Title: "B"},
	}
	resp := api.Put("/v0/namespaces/default/agents/bad/latest", body)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "metadata.version")
}

func TestResourceRegister_ValidationRejectsHTTPWebsite(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "ins", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "I", WebsiteURL: "http://example.com"}, // http not allowed
	}
	resp := api.Put("/v0/namespaces/default/agents/ins/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "spec.websiteUrl")
}

func TestResourceRegister_ResolverDetectsDanglingRef(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	agentStore := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	mcpStore := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")

	// Resolver: only MCPServer "tools" in namespace "default" exists.
	resolver := func(ctx context.Context, ref v1alpha1.ResourceRef) error {
		if ref.Kind != v1alpha1.KindMCPServer {
			return nil
		}
		_, err := mcpStore.Get(ctx, ref.Namespace, ref.Name, ref.Version)
		return err
	}

	// Seed the one existing MCPServer.
	_, err := mcpStore.Upsert(context.Background(), "default", "tools", "v1",
		mustSpec(t, v1alpha1.MCPServerSpec{Title: "T"}), v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      agentStore,
		Resolver:   resolver,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

	// Reference a missing MCPServer.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "dangling", Version: "v1"},
		Spec: v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{
				{Kind: v1alpha1.KindMCPServer, Name: "tools", Version: "v1"},
				{Kind: v1alpha1.KindMCPServer, Name: "missing", Version: "v1"},
			},
		},
	}
	resp := api.Put("/v0/namespaces/default/agents/dangling/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "spec.mcpServers[1]")
}

// mustSpec is a test helper duplicated from the database package tests.
// Kept local so we don't create a test-util cycle.
func mustSpec(t *testing.T, spec any) []byte {
	t.Helper()
	b, err := json.Marshal(spec)
	require.NoError(t, err)
	return b
}

// TestResourceRegister_UniqueRemoteURLsAcrossAgents exercises the
// cross-row uniqueness check: two Agents can't claim the same remote URL,
// but multiple versions of the same Agent can.
func TestResourceRegister_UniqueRemoteURLsAcrossAgents(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	stores := database.NewV1Alpha1Stores(pool)

	checker := database.NewV1Alpha1UniqueRemoteURLsChecker(stores)

	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:                    v1alpha1.KindAgent,
		BasePrefix:              "/v0",
		Store:                   stores[v1alpha1.KindAgent],
		UniqueRemoteURLsChecker: checker,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

	newAgent := func(name, version, url string) v1alpha1.Agent {
		return v1alpha1.Agent{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Version: version},
			Spec: v1alpha1.AgentSpec{
				Remotes: []v1alpha1.AgentRemote{{Type: "sse", URL: url}},
			},
		}
	}

	sharedURL := "https://api.example.com/shared"

	// First Agent claims the URL — OK.
	resp := api.Put("/v0/namespaces/default/agents/alice/v1", newAgent("alice", "v1", sharedURL))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Same Agent, different version, same URL — OK (same name).
	resp = api.Put("/v0/namespaces/default/agents/alice/v2", newAgent("alice", "v2", sharedURL))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Different Agent, same URL — 409 Conflict.
	resp = api.Put("/v0/namespaces/default/agents/bob/v1", newAgent("bob", "v1", sharedURL))
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), sharedURL)
	require.Contains(t, resp.Body.String(), "alice")

	// Different Agent, different URL — OK.
	resp = api.Put("/v0/namespaces/default/agents/bob/v1", newAgent("bob", "v1", "https://api.example.com/other"))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

// TestResourceRegister_UniqueRemoteURLsPerKind confirms that uniqueness
// is per-Kind: an Agent and an MCPServer may share a URL.
func TestResourceRegister_UniqueRemoteURLsPerKind(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	stores := database.NewV1Alpha1Stores(pool)
	checker := database.NewV1Alpha1UniqueRemoteURLsChecker(stores)

	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind: v1alpha1.KindAgent, BasePrefix: "/v0",
		Store: stores[v1alpha1.KindAgent], UniqueRemoteURLsChecker: checker,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
	resource.Register[*v1alpha1.MCPServer](api, resource.Config{
		Kind: v1alpha1.KindMCPServer, BasePrefix: "/v0", PluralKind: "mcpservers",
		Store: stores[v1alpha1.KindMCPServer], UniqueRemoteURLsChecker: checker,
	}, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })

	sharedURL := "https://mcp.example.com/endpoint"

	agent := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "shared", Version: "v1"},
		Spec: v1alpha1.AgentSpec{
			Remotes: []v1alpha1.AgentRemote{{Type: "sse", URL: sharedURL}},
		},
	}
	resp := api.Put("/v0/namespaces/default/agents/shared/v1", agent)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	mcp := v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "shared", Version: "v1"},
		Spec: v1alpha1.MCPServerSpec{
			Remotes: []v1alpha1.MCPTransport{{Type: "streamable-http", URL: sharedURL}},
		},
	}
	// Same URL on a different Kind is allowed.
	resp = api.Put("/v0/namespaces/default/mcpservers/shared/v1", mcp)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

func TestResourceRegister_SoftDeleteFinalizerGC(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Apply with a finalizer — finalizers move through metadata.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Namespace:  "default",
			Name:       "fin",
			Version:    "v1",
			Finalizers: []string{"cleanup.agentregistry.dev/local-deploy"},
		},
		Spec: v1alpha1.AgentSpec{Title: "Fin"},
	}
	resp := api.Put("/v0/namespaces/default/agents/fin/v1", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var gotAgent v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, []string{"cleanup.agentregistry.dev/local-deploy"}, gotAgent.Metadata.Finalizers)

	// DELETE sets deletionTimestamp; row remains because finalizers is non-empty.
	resp = api.Delete("/v0/namespaces/default/agents/fin/v1")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// Row is still fetchable with deletionTimestamp set.
	resp = api.Get("/v0/namespaces/default/agents/fin/v1")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.NotNil(t, gotAgent.Metadata.DeletionTimestamp)
	require.Len(t, gotAgent.Metadata.Finalizers, 1)

	// GC before finalizer drained: no-op.
	purged, err := store.PurgeFinalized(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 0, purged)

	// Drain the finalizer via Store.PatchFinalizers (reconciler's path).
	err = store.PatchFinalizers(t.Context(), "default", "fin", "v1", func(f []string) []string { return nil })
	require.NoError(t, err)

	// GC now hard-deletes the row.
	purged, err = store.PurgeFinalized(t.Context())
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	// Final GET: 404.
	resp = api.Get("/v0/namespaces/default/agents/fin/v1")
	require.Equal(t, http.StatusNotFound, resp.Code)
}

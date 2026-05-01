//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// mustSpecJSON marshals a kind-specific spec into JSONB for direct
// Store.Upsert calls in tests that bypass the HTTP path.
func mustSpecJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	require.NoError(t, err)
	return json.RawMessage(b)
}

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

	pool := v1alpha1store.NewTestPool(t)
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
			Title:  "Alice",
			Source: &v1alpha1.AgentSource{Image: "ghcr.io/example/alice:1.0.0"},
		},
	}
	resp := api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var gotAgent v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, v1alpha1.GroupVersion, gotAgent.APIVersion)
	require.Equal(t, v1alpha1.KindAgent, gotAgent.Kind)
	// Wire strips namespace="default"; the client observes empty. Use
	// NamespaceOrDefault for display / id composition.
	require.Equal(t, "default", gotAgent.Metadata.NamespaceOrDefault())
	require.Equal(t, "alice", gotAgent.Metadata.Name)
	require.Equal(t, "v1.0.0", gotAgent.Metadata.Version)
	// Generation is hidden from the wire (json:"-"), so the client decode
	// sees its zero value. Internal consumers (coordinator, reconcilers)
	// read the DB column directly.
	require.Equal(t, "Alice", gotAgent.Spec.Title)
	require.Equal(t, "platform", gotAgent.Metadata.Labels["team"])

	// GET exact version.
	resp = api.Get("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "alice", gotAgent.Metadata.Name)

	// GET latest.
	resp = api.Get("/v0/agents/alice")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "v1.0.0", gotAgent.Metadata.Version)

	// LIST in namespace with label selector.
	resp = api.Get("/v0/agents?labels=team%3Dplatform")
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

	// Re-apply with same spec — generation must stay at 1. Generation
	// is internal-only so the assertion goes through the Store directly
	// (the wire response omits generation via json:"-").
	resp = api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	row, err := store.Get(t.Context(), "default", "alice", "v1.0.0")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Metadata.Generation, "no-op apply preserves generation")

	// PUT with mutated spec — generation bumps to 2.
	putBody.Spec.Title = "Alice v2"
	resp = api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "Alice v2", gotAgent.Spec.Title)
	row, err = store.Get(t.Context(), "default", "alice", "v1.0.0")
	require.NoError(t, err)
	require.EqualValues(t, 2, row.Metadata.Generation)

	// DELETE — finalizer-free row hard-deletes immediately. (The
	// soft-delete + drain dance only kicks in for finalizer-bearing
	// rows; see TestResourceRegister_DeleteHardDeletesFinalizerFree.)
	resp = api.Delete("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// GetLatest returns 404 — row is gone.
	resp = api.Get("/v0/agents/alice")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())

	// GET on the exact version returns 404 too.
	resp = api.Get("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusNotFound, resp.Code)

	// List is empty.
	resp = api.Get("/v0/agents")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Empty(t, list.Items)

	// includeTerminating=true also empty since there's no terminating row.
	resp = api.Get("/v0/agents?includeTerminating=true")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Empty(t, list.Items)
}

func TestResourceRegister_AgentNamespaceIsolation(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
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

	resp := api.Put("/v0/agents/shared/v1?namespace=team-a", bodyTeamA)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	resp = api.Put("/v0/agents/shared/v1?namespace=team-b", bodyTeamB)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Namespaced GETs resolve the right one.
	var got v1alpha1.Agent
	resp = api.Get("/v0/agents/shared/v1?namespace=team-a")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "A's", got.Spec.Title)

	resp = api.Get("/v0/agents/shared/v1?namespace=team-b")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "B's", got.Spec.Title)

	// Cross-namespace list returns both (?namespace=all widens scope).
	resp = api.Get("/v0/agents?namespace=all")
	require.Equal(t, http.StatusOK, resp.Code)
	var list struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 2)

	// Namespaced list returns one.
	resp = api.Get("/v0/agents?namespace=team-a")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "team-a", list.Items[0].Metadata.Namespace)
}

func TestResourceRegister_AgentListCursorPagination(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	for _, name := range []string{"one", "two", "three"} {
		body := v1alpha1.Agent{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Version: "v1"},
			Spec:     v1alpha1.AgentSpec{Title: name},
		}
		resp := api.Put(fmt.Sprintf("/v0/agents/%s/v1", url.PathEscape(name)), body)
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
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	resp := api.Get("/v0/agents?cursor=not-a-valid-cursor")
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	require.Contains(t, resp.Body.String(), "invalid cursor")
}

// TestResourceRegister_ListFilter exercises the per-row authz hook by
// wiring a ListFilter that only returns rows whose name starts with
// "ok-". Three rows are seeded; the unfiltered list returns all three,
// the filtered list returns just the two matches.
func TestResourceRegister_ListFilter(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	for _, name := range []string{"ok-one", "ok-two", "blocked-three"} {
		_, err := store.Upsert(t.Context(), "default", name, "v1",
			mustSpecJSON(t, v1alpha1.AgentSpec{Title: name}),
			v1alpha1store.UpsertOpts{})
		require.NoError(t, err)
	}

	// Without filter — sees all three.
	_, plainAPI := humatest.New(t)
	registerAgent(plainAPI, store)
	plainResp := plainAPI.Get("/v0/agents")
	require.Equal(t, http.StatusOK, plainResp.Code, plainResp.Body.String())
	var plain struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(plainResp.Body.Bytes(), &plain))
	require.Len(t, plain.Items, 3, "no-filter list must return every row")

	// With filter — sees only ok-* rows. The fragment uses
	// `name LIKE $1` so the rebaser bumps $1 past the Store's internal
	// placeholders (deletion_timestamp + label predicates) automatically.
	_, filteredAPI := humatest.New(t)
	resource.Register[*v1alpha1.Agent](filteredAPI, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
		ListFilter: func(_ context.Context, _ resource.AuthorizeInput) (string, []any, error) {
			return "name LIKE $1", []any{"ok-%"}, nil
		},
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
	filteredResp := filteredAPI.Get("/v0/agents")
	require.Equal(t, http.StatusOK, filteredResp.Code, filteredResp.Body.String())
	var filtered struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(filteredResp.Body.Bytes(), &filtered))
	require.Len(t, filtered.Items, 2, "ListFilter must restrict the result set")
	for _, a := range filtered.Items {
		require.True(t, strings.HasPrefix(a.Metadata.Name, "ok-"))
	}
}

func TestResourceRegister_AgentWrongKindRejected(t *testing.T) {
	t.Helper()

	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Body carries Kind: "Skill" but PUT targets the agents handler.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: "Skill"},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "bob", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "wrong kind"},
	}
	resp := api.Put("/v0/agents/bob/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestResourceRegister_AgentPathMismatchRejected(t *testing.T) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "mismatched", Version: "v1"},
	}
	resp := api.Put("/v0/agents/alice/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, fmt.Sprintf("body=%s", resp.Body.String()))
}

func TestResourceRegister_ValidationRejectsBadVersion(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "bad", Version: "latest"},
		Spec:     v1alpha1.AgentSpec{Title: "B"},
	}
	resp := api.Put("/v0/agents/bad/latest", body)
	require.Equal(t, http.StatusBadRequest, resp.Code)
	require.Contains(t, resp.Body.String(), "metadata.version")
}

func TestResourceRegister_ResolverDetectsDanglingRef(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
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
	resp := api.Put("/v0/agents/dangling/v1", body)
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

// TestResourceRegister_DeleteHardDeletesFinalizerFree pins the K8s
// fast-path: rows with no finalizers hard-delete synchronously on
// DELETE. Without it, "DELETE then PUT same identity" hits
// ErrTerminating until the (currently non-existent) GC purges the row.
// Reported by josh-pritchard on PR #455 ("Soft-delete blocks re-apply
// for every v1alpha1 kind"); fixed at the Store layer.
func TestResourceRegister_DeleteHardDeletesFinalizerFree(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Create the row via the wire.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "soft", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "Soft"},
	}
	resp := api.Put("/v0/agents/soft/v1", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// DELETE on a finalizer-free row hard-deletes immediately.
	resp = api.Delete("/v0/agents/soft/v1")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// GET returns 404 — row is gone, not terminating.
	resp = api.Get("/v0/agents/soft/v1")
	require.Equal(t, http.StatusNotFound, resp.Code)

	// Re-apply with the same (name, version) succeeds — no
	// "object is terminating" race since the row is fully removed.
	resp = api.Put("/v0/agents/soft/v1", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Generation is server-managed (json:"-") so it doesn't appear on
	// the wire; check directly against the store. A re-applied row
	// after hard-delete is a fresh create, so generation resets to 1.
	row, err := store.Get(t.Context(), "default", "soft", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 1, row.Metadata.Generation,
		"re-apply after hard-delete must reset generation to 1")
}

// TestResourceRegister_PostUpsertFailureLeavesPersistedRow pins the
// documented (pre-Phase-2-KRT) contract: when PostUpsert returns an
// error, Store.Upsert has already committed and the row is persisted;
// the caller sees a 500, but a follow-up GetLatest still returns the
// row with whatever Status the previous reconcile (or zero-value) left.
//
// The risk this guards against is silently moving the contract — e.g.
// adding a "stamp Failed condition / hard-delete the row" branch
// without updating the godoc on Config.PostUpsert. Tests pin the
// behavior so future changes are forced through documentation +
// reviewer awareness.
func TestResourceRegister_PostUpsertFailureLeavesPersistedRow(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	hookCalls := 0
	hookErr := fmt.Errorf("simulated platform-adapter failure")
	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
		PostUpsert: func(ctx context.Context, obj v1alpha1.Object) error {
			hookCalls++
			return hookErr
		},
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "halfapplied", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "Half"},
	}

	// PUT → 500. Hook fired exactly once.
	resp := api.Put("/v0/agents/halfapplied/v1", body)
	require.Equal(t, http.StatusInternalServerError, resp.Code, resp.Body.String())
	require.Equal(t, 1, hookCalls, "PostUpsert must fire exactly once on the failing apply")

	// Row persists despite the hook failure: subsequent GET returns 200.
	resp = api.Get("/v0/agents/halfapplied/v1")
	require.Equal(t, http.StatusOK, resp.Code,
		"contract: Store.Upsert commits before the hook, so a hook failure leaves the row persisted")

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "halfapplied", got.Metadata.Name)
	require.Equal(t, "Half", got.Spec.Title,
		"spec is the just-applied value — the upsert succeeded under the hood")

	// Re-apply with identical spec: the no-op upsert at the Store
	// layer does NOT short-circuit PostUpsert — the handler fires
	// the hook unconditionally after Upsert returns. This is the
	// operator-friendly retry path: a transient platform-adapter
	// failure clears as soon as a re-apply succeeds, with no spec
	// bump required. Pin the behavior so a future "skip hook on
	// no-op" optimization has to update the godoc + this test.
	hookCalls = 0
	resp = api.Put("/v0/agents/halfapplied/v1", body)
	require.Equal(t, http.StatusInternalServerError, resp.Code,
		"identical-spec re-apply still fires the hook (and 500s if the hook still errors)")
	require.Equal(t, 1, hookCalls,
		"contract: hook re-fires on every PUT, including no-op upserts; this is the retry path")

	// Now make the hook succeed and re-apply: success path returns 200,
	// hook fired again (third call total, counter=1 since reset), row
	// readable through the regular GET.
	hookErr = nil
	hookCalls = 0
	resp = api.Put("/v0/agents/halfapplied/v1", body)
	require.Equal(t, http.StatusOK, resp.Code,
		"once the platform-adapter clears, identical-spec re-apply succeeds without a spec bump")
	require.Equal(t, 1, hookCalls)
}

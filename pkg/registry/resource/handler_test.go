//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// registerAgent wires the generic resource handler for *v1alpha1.Agent and
// the multi-doc apply endpoint onto the given Huma API, against the supplied
// Store. It's a test-local helper so we don't pull the full registry_app
// into these tests.
//
// Direct PUT on the per-kind item URL is no longer registered for
// content-registry kinds (Agent, MCPServer, RemoteMCPServer, Skill,
// Prompt) — POST /v0/apply is the single create/update entry point. The
// helper wires both so tests can drive applies through /v0/apply and
// reads/deletes through the per-kind GET/DELETE.
func registerAgent(api huma.API, store *v1alpha1store.Store) {
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores:     map[string]*v1alpha1store.Store{v1alpha1.KindAgent: store},
	})
}

func registerProvider(api huma.API, store *v1alpha1store.Store) {
	resource.Register[*v1alpha1.Provider](api, resource.Config{
		Kind:       v1alpha1.KindProvider,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
}

// applyAgentYAML POSTs a single Agent document to /v0/apply and returns
// the per-document ApplyResult. Used by the rewritten tests in this file
// since direct PUT on a content-kind URL is no longer registered.
func applyAgentYAML(t *testing.T, api humatest.TestAPI, yaml string) arv0.ApplyResult {
	t.Helper()
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(yaml))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1, "expected exactly one ApplyResult; got: %s", resp.Body.String())
	return out.Results[0]
}

// newTestPool is defined in database/store_v1alpha1_testutil.go. Each test
// gets its own isolated DB.
func TestResourceRegister_AgentCRUD(t *testing.T) {
	t.Helper()

	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Apply a new agent in the default namespace via POST /v0/apply.
	createYAML := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: alice
  labels:
    team: platform
spec:
  title: Alice
  source:
    image: ghcr.io/example/alice:1.0.0
`
	res := applyAgentYAML(t, api, createYAML)
	require.Equal(t, arv0.ApplyStatusCreated, res.Status, "first apply must report created")
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)

	// GET exact tag.
	resp := api.Get("/v0/agents/alice/latest")
	require.Equal(t, http.StatusOK, resp.Code)
	var gotAgent v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, v1alpha1.GroupVersion, gotAgent.APIVersion)
	require.Equal(t, v1alpha1.KindAgent, gotAgent.Kind)
	require.Equal(t, "default", gotAgent.Metadata.NamespaceOrDefault())
	require.Equal(t, "alice", gotAgent.Metadata.Name)
	require.Equal(t, v1alpha1store.DefaultTag(), gotAgent.Metadata.Tag)
	require.Equal(t, "Alice", gotAgent.Spec.Title)
	require.Equal(t, "platform", gotAgent.Metadata.Labels["team"])

	// GET latest.
	resp = api.Get("/v0/agents/alice")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, v1alpha1store.DefaultTag(), gotAgent.Metadata.Tag)

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

	// Re-apply with the same spec is a no-op at the Store layer; the
	// row remains at tag latest.
	res = applyAgentYAML(t, api, createYAML)
	require.Equal(t, arv0.ApplyStatusUnchanged, res.Status, "no-op re-apply must report unchanged")
	latest, err := store.GetLatest(t.Context(), "default", "alice")
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), latest.Metadata.Tag)

	// Apply with mutated spec under the same default tag replaces the row.
	updateYAML := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: alice
  labels:
    team: platform
spec:
  title: Alice v2
  source:
    image: ghcr.io/example/alice:1.0.0
`
	res = applyAgentYAML(t, api, updateYAML)
	require.Equal(t, arv0.ApplyStatusConfigured, res.Status, "spec change must replace latest")
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)
	latest, err = store.GetLatest(t.Context(), "default", "alice")
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), latest.Metadata.Tag)
	// store.GetLatest returns spec as raw JSON; read back through the
	// per-kind GET handler for the typed view.
	resp = api.Get("/v0/agents/alice/latest")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, "Alice v2", gotAgent.Spec.Title)

	// DELETE — tagged-artifact rows have no finalizers, so DELETE
	// hard-deletes the targeted tag immediately.
	resp = api.Delete("/v0/agents/alice/latest")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// GetLatest returns 404 — row is gone.
	resp = api.Get("/v0/agents/alice")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())

	// GET on the exact tag returns 404 too.
	resp = api.Get("/v0/agents/alice/latest")
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

func TestResourceRegister_DeleteTaggedPassesTagToAuthorizer(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	_, err := store.Upsert(t.Context(), &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Tag: "stable"},
		Spec:     v1alpha1.AgentSpec{Title: "Stable Alice"},
	})
	require.NoError(t, err)

	var seen resource.AuthorizeInput
	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
		Authorize: func(_ context.Context, in resource.AuthorizeInput) error {
			if in.Verb == "delete" {
				seen = in
			}
			return nil
		},
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

	resp := api.Delete("/v0/agents/alice/stable")
	require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())
	require.Equal(t, "delete", seen.Verb)
	require.Equal(t, v1alpha1.KindAgent, seen.Kind)
	require.Equal(t, "default", seen.Namespace)
	require.Equal(t, "alice", seen.Name)
	require.Equal(t, "stable", seen.Tag)
}

func TestResourceRegister_AgentNamespaceIsolation(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Same name in two different namespaces — no conflict. Apply each
	// via POST /v0/apply; metadata.namespace on the doc is authoritative.
	teamA := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: team-a
  name: shared
spec:
  title: A's
`
	teamB := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: team-b
  name: shared
spec:
  title: B's
`
	resA := applyAgentYAML(t, api, teamA)
	require.Equal(t, arv0.ApplyStatusCreated, resA.Status)
	resB := applyAgentYAML(t, api, teamB)
	require.Equal(t, arv0.ApplyStatusCreated, resB.Status)

	// Namespaced GETs resolve the right one.
	var got v1alpha1.Agent
	resp := api.Get("/v0/agents/shared/latest?namespace=team-a")
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "A's", got.Spec.Title)

	resp = api.Get("/v0/agents/shared/latest?namespace=team-b")
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
		yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: %s
spec:
  title: %s
`, name, name)
		res := applyAgentYAML(t, api, yaml)
		require.Equal(t, arv0.ApplyStatusCreated, res.Status)
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

// TestResourceRegister_AgentListTags pins the GET /v0/{plural}/{name}/tags
// contract: every non-deleted tag row for (namespace, name) is returned.
func TestResourceRegister_AgentListTags(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "foo", Tag: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "v1"},
	}
	_, err := store.Upsert(t.Context(), &body)
	require.NoError(t, err)
	body.Metadata.Tag = "v2"
	body.Spec.Title = "v2"
	_, err = store.Upsert(t.Context(), &body)
	require.NoError(t, err)

	resp := api.Get("/v0/agents/foo/tags")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var list struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 2, "both tags should be returned")
	// Ordering: newest updated tag first.
	require.Equal(t, "v2", list.Items[0].Metadata.Tag)
	require.Equal(t, "v2", list.Items[0].Spec.Title)
	require.Equal(t, "v1", list.Items[1].Metadata.Tag)
	require.Equal(t, "v1", list.Items[1].Spec.Title)

	// Unknown name → 200 with empty items (list semantics: a
	// nonexistent name is just an empty result set, not an error).
	resp = api.Get("/v0/agents/missing/tags")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var empty struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &empty))
	require.Empty(t, empty.Items)
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
		_, err := store.Upsert(t.Context(), &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name},
			Spec:     v1alpha1.AgentSpec{Title: name},
		})
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

// TestResourceRegister_PutNotRegisteredForContentKinds pins the
// post-redesign contract: direct PUT on the per-kind item URL is no
// longer registered for content-registry kinds (Agent, MCPServer,
// RemoteMCPServer, Skill, Prompt). POST /v0/apply is the single
// create/update entry point — user-controlled tags live in metadata.tag rather
// than the URL segment of a direct PUT. Provider/Deployment mutable-object
// stores still expose direct namespace/name PUT.
//
// The test issues a PUT against the agents handler and expects 405
// (Method Not Allowed) — the path exists for GET / DELETE, but PUT is
// not in the registered method set. The Allow header on the response
// confirms PUT is excluded.
func TestResourceRegister_PutNotRegisteredForContentKinds(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	resp := api.Put("/v0/agents/foo/1", v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "foo"},
		Spec:     v1alpha1.AgentSpec{Title: "Foo"},
	})
	require.Equal(t, http.StatusMethodNotAllowed, resp.Code,
		"PUT route must not be registered for content-registry kinds; got %d body=%s",
		resp.Code, resp.Body.String())
	require.NotContains(t, resp.Header().Get("Allow"), "PUT",
		"Allow header must not list PUT for content-registry kinds; got %q",
		resp.Header().Get("Allow"))

	// Also sanity-check via raw httptest (no Huma wrapping) so the
	// assertion is independent of humatest's request shaping.
	httpReq := httptest.NewRequest(http.MethodPut, "/v0/agents/foo/1", strings.NewReader(
		`{"apiVersion":"ar.dev/v1alpha1","kind":"Agent","metadata":{"name":"foo"},"spec":{}}`))
	httpReq.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	api.Adapter().ServeHTTP(rec, httpReq)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code,
		"raw PUT against content-registry kind must return 405; got %d body=%s",
		rec.Code, rec.Body.String())
}

func TestResourceRegister_MutableObjectUsesNameOnlyRoute(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewMutableObjectStore(pool, "v1alpha1.providers")

	_, api := humatest.New(t)
	registerProvider(api, store)

	provider := v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: "default",
			Name:      "local-test",
		},
		Spec: v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformLocal},
	}

	resp := api.Put("/v0/providers/local-test", provider)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NotContains(t, resp.Body.String(), `"version"`, "mutable object response must not expose metadata.version")

	resp = api.Get("/v0/providers/local-test")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NotContains(t, resp.Body.String(), `"version"`, "mutable object get must not expose metadata.version")

	resp = api.Put("/v0/providers/local-test/1", provider)
	require.Equal(t, http.StatusNotFound, resp.Code, "mutable object version route must not be registered")

	resp = api.Delete("/v0/providers/local-test")
	require.Equal(t, http.StatusNoContent, resp.Code, resp.Body.String())
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
		identity := ref.Tag
		if identity == "" {
			identity = v1alpha1store.DefaultTag()
		}
		_, err := mcpStore.Get(ctx, ref.Namespace, ref.Name, identity)
		return err
	}

	// Seed the one existing MCPServer.
	_, err := mcpStore.Upsert(context.Background(), &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "tools"},
		Spec:     v1alpha1.MCPServerSpec{Title: "T"},
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix: "/v0",
		Stores: map[string]*v1alpha1store.Store{
			v1alpha1.KindAgent:     agentStore,
			v1alpha1.KindMCPServer: mcpStore,
		},
		Resolver: resolver,
	})

	// Reference a missing MCPServer via POST /v0/apply.
	yaml := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: dangling
spec:
  mcpServers:
    - kind: MCPServer
      name: tools
      tag: latest
    - kind: MCPServer
      name: missing
      tag: latest
`
	resp := api.Post("/v0/apply", "Content-Type: application/yaml", strings.NewReader(yaml))
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out struct {
		Results []arv0.ApplyResult `json:"results"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	require.Len(t, out.Results, 1)
	require.Equal(t, arv0.ApplyStatusFailed, out.Results[0].Status)
	require.Contains(t, out.Results[0].Error, "spec.mcpServers[1]")
}

// TestResourceRegister_DeleteHardDeletesFinalizerFree pins the K8s
// fast-path: rows with no finalizers hard-delete synchronously on
// DELETE. Without it, "DELETE then apply same identity" hits
// ErrTerminating until the (currently non-existent) GC purges the row.
// Reported by josh-pritchard on PR #455 ("Soft-delete blocks re-apply
// for every v1alpha1 kind"); fixed at the Store layer.
func TestResourceRegister_DeleteHardDeletesFinalizerFree(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Create the row via POST /v0/apply.
	createYAML := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: soft
spec:
  title: Soft
`
	res := applyAgentYAML(t, api, createYAML)
	require.Equal(t, arv0.ApplyStatusCreated, res.Status)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)

	// DELETE on a finalizer-free row hard-deletes immediately. DELETE
	// stays per-kind for content-registry kinds (CLI uses it for
	// "arctl delete agent foo --tag latest").
	resp := api.Delete("/v0/agents/soft/latest")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// GET returns 404 — row is gone, not terminating.
	resp = api.Get("/v0/agents/soft/latest")
	require.Equal(t, http.StatusNotFound, resp.Code)

	// Re-apply with the same logical identity succeeds — no
	// "object is terminating" race since the row is fully removed.
	// A fresh create after hard-delete recreates the latest tag.
	res = applyAgentYAML(t, api, createYAML)
	require.Equal(t, arv0.ApplyStatusCreated, res.Status)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag,
		"re-apply after hard-delete is a fresh insert at tag latest")

	row, err := store.Get(t.Context(), "default", "soft", v1alpha1store.DefaultTag())
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), row.Metadata.Tag)
}

// TestResourceRegister_PostUpsertFailureLeavesPersistedRow pins the
// documented (pre-Phase-2-KRT) contract: when PostUpsert returns an
// error, Store.Upsert has already committed and the row is persisted;
// the caller sees a failed result, but a follow-up GetLatest still
// returns the row with whatever Status the previous reconcile (or
// zero-value) left.
//
// The risk this guards against is silently moving the contract — e.g.
// adding a "stamp Failed condition / hard-delete the row" branch
// without updating the godoc on ApplyConfig.PostUpserts. Tests pin the
// behavior so future changes are forced through documentation +
// reviewer awareness.
func TestResourceRegister_PostUpsertFailureLeavesPersistedRow(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	hookCalls := 0
	hookErr := fmt.Errorf("simulated platform-adapter failure")
	hook := func(ctx context.Context, obj v1alpha1.Object) error {
		hookCalls++
		return hookErr
	}

	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
	resource.RegisterApply(api, resource.ApplyConfig{
		BasePrefix:  "/v0",
		Stores:      map[string]*v1alpha1store.Store{v1alpha1.KindAgent: store},
		PostUpserts: map[string]func(context.Context, v1alpha1.Object) error{v1alpha1.KindAgent: hook},
	})

	yaml := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  namespace: default
  name: halfapplied
spec:
  title: Half
`

	// Apply → failed result. Hook fired exactly once.
	res := applyAgentYAML(t, api, yaml)
	require.Equal(t, arv0.ApplyStatusFailed, res.Status)
	require.Contains(t, res.Error, "simulated platform-adapter failure")
	require.Equal(t, 1, hookCalls, "PostUpsert must fire exactly once on the failing apply")

	// Row persists despite the hook failure: subsequent GET returns 200.
	resp := api.Get("/v0/agents/halfapplied/latest")
	require.Equal(t, http.StatusOK, resp.Code,
		"contract: Store.Upsert commits before the hook, so a hook failure leaves the row persisted")

	var got v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "halfapplied", got.Metadata.Name)
	require.Equal(t, "Half", got.Spec.Title,
		"spec is the just-applied value — the upsert succeeded under the hood")

	// Re-apply with identical spec: the no-op upsert at the Store
	// layer does NOT short-circuit PostUpsert — the apply path fires
	// the hook unconditionally after Upsert returns. This is the
	// operator-friendly retry path: a transient platform-adapter
	// failure clears as soon as a re-apply succeeds, with no spec
	// bump required. Pin the behavior so a future "skip hook on
	// no-op" optimization has to update the godoc + this test.
	hookCalls = 0
	res = applyAgentYAML(t, api, yaml)
	require.Equal(t, arv0.ApplyStatusFailed, res.Status,
		"identical-spec re-apply still fires the hook (and fails if the hook still errors)")
	require.Equal(t, 1, hookCalls,
		"contract: hook re-fires on every apply, including no-op upserts; this is the retry path")

	// Now make the hook succeed and re-apply: success path returns
	// unchanged (Store.Upsert no-op'd), hook fired again, row readable
	// through the regular GET.
	hookErr = nil
	hookCalls = 0
	res = applyAgentYAML(t, api, yaml)
	require.NotEqual(t, arv0.ApplyStatusFailed, res.Status,
		"once the platform-adapter clears, identical-spec re-apply succeeds without a spec bump")
	require.Equal(t, 1, hookCalls)
}

// TestResourceRegister_IncludeTerminatingByDefault pins the opt-in
// behavior: a kind registered with IncludeTerminatingByDefault=true
// surfaces terminating rows on plain LIST (no ?includeTerminating=true
// needed), and ?latestOnly=true remains a no-op for mutable objects because
// namespace/name is already unique.
func TestResourceRegister_IncludeTerminatingByDefault(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewMutableObjectStore(pool, "v1alpha1.providers")
	const testNamespace = "terminating-test"

	_, api := humatest.New(t)
	resource.Register[*v1alpha1.Provider](api, resource.Config{
		Kind:                        v1alpha1.KindProvider,
		BasePrefix:                  "/v0",
		Store:                       store,
		IncludeTerminatingByDefault: true,
	}, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })

	// Seed a row, attach a finalizer, then soft-delete so the row goes
	// terminating (deletion_timestamp set).
	_, err := store.Upsert(t.Context(), &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: testNamespace, Name: "draining"},
		Spec:     v1alpha1.ProviderSpec{Platform: "noop"},
	})
	require.NoError(t, err)

	require.NoError(t, store.PatchFinalizers(t.Context(), testNamespace, "draining", "",
		func([]string) []string { return []string{"finalizer.example.com"} }))
	require.NoError(t, store.Delete(t.Context(), testNamespace, "draining", ""))

	var list struct {
		Items      []v1alpha1.Provider `json:"items"`
		NextCursor string              `json:"nextCursor,omitempty"`
	}

	// Plain LIST with no ?includeTerminating still returns the terminating
	// row because the kind opted in via IncludeTerminatingByDefault.
	resp := api.Get("/v0/providers?namespace=" + testNamespace)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "draining", list.Items[0].Metadata.Name)
	require.NotNil(t, list.Items[0].Metadata.DeletionTimestamp,
		"opt-in default must surface the deletionTimestamp so operators see in-flight teardown")

	// LatestOnly is a no-op for mutable objects, so the terminating row remains
	// visible because the kind opted into IncludeTerminatingByDefault.
	resp = api.Get("/v0/providers?namespace=" + testNamespace + "&latestOnly=true")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "draining", list.Items[0].Metadata.Name)
}

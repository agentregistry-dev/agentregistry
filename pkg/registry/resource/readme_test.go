//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	builtins "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/builtins"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func registerAgentWithReadme(api huma.API, store *v1alpha1store.Store) {
	cfg := resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
	}
	newObj := func() *v1alpha1.Agent { return &v1alpha1.Agent{} }
	// Readme routes first: the literal `/{name}/readme` needs to beat
	// the generic `/{name}/{version}` route at the shared depth.
	resource.RegisterReadme[*v1alpha1.Agent](api, cfg, newObj, func(obj *v1alpha1.Agent) *v1alpha1.Readme {
		return obj.Spec.Readme
	})
	resource.Register[*v1alpha1.Agent](api, cfg, newObj)
}

func TestResourceRegister_AgentReadmeRoutesAndListProjection(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	registerAgentWithReadme(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "alice", Version: "v1.0.0"},
		Spec: v1alpha1.AgentSpec{
			Title: "Alice",
			Readme: &v1alpha1.Readme{
				ContentType: "text/markdown",
				Content:     "# Alice\n\nLong-form docs.",
			},
		},
	}

	resp := api.Put("/v0/agents/alice/v1.0.0", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	resp = api.Get("/v0/agents/alice/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var gotReadme v1alpha1.Readme
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotReadme))
	require.Equal(t, "text/markdown", gotReadme.ContentType)
	require.Equal(t, "# Alice\n\nLong-form docs.", gotReadme.Content)

	resp = api.Get("/v0/agents/alice/versions/v1.0.0/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotReadme))
	require.Equal(t, "# Alice\n\nLong-form docs.", gotReadme.Content)

	resp = api.Get("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var exact v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &exact))
	require.NotNil(t, exact.Spec.Readme)
	require.Equal(t, "# Alice\n\nLong-form docs.", exact.Spec.Readme.Content)

	resp = api.Get("/v0/agents")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items []v1alpha1.Agent `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.NotNil(t, list.Items[0].Spec.Readme)
	require.Empty(t, list.Items[0].Spec.Readme.Content, "list responses must strip heavy readme bodies")
}

func TestRegisterBuiltins_LegacyServerReadmeAlias(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	stores := v1alpha1store.NewV1Alpha1Stores(pool)

	_, api := humatest.New(t)
	builtins.RegisterBuiltins(api, "/v0", stores, nil, nil, nil, nil, builtins.PerKindHooks{})

	server := v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindMCPServer},
		Metadata: v1alpha1.ObjectMeta{Namespace: v1alpha1.DefaultNamespace, Name: "fetch", Version: "v1.0.0"},
		Spec: v1alpha1.MCPServerSpec{
			Title: "Fetch",
			Readme: &v1alpha1.Readme{
				ContentType: "text/markdown",
				Content:     "# Fetch\n\nServer docs.",
			},
		},
	}

	resp := api.Put("/v0/mcpservers/fetch/v1.0.0", server)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	resp = api.Get("/v0/servers/fetch/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got v1alpha1.Readme
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "# Fetch\n\nServer docs.", got.Content)

	resp = api.Get("/v0/servers/fetch/versions/v1.0.0/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "# Fetch\n\nServer docs.", got.Content)
}

// TestRegisterReadme_RespectsAuthorize pins the row-level RBAC
// invariant for the readme subresource: a deny on (Kind, Name) at the
// regular GET handler MUST also block the readme path. Without this
// gate, an enterprise tenant could read README content (markdown body
// frequently containing setup instructions, internal hostnames,
// contact info) for resources they don't have grants for.
func TestRegisterReadme_RespectsAuthorize(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	cfg := resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
		// Deny readme reads of the secret agent; allow everyone else.
		Authorize: func(ctx context.Context, in resource.AuthorizeInput) error {
			if in.Verb == "get" && in.Name == "secret" {
				return huma.Error403Forbidden(fmt.Sprintf("denied: %s/%s", in.Kind, in.Name))
			}
			return nil
		},
	}
	newObj := func() *v1alpha1.Agent { return &v1alpha1.Agent{} }
	resource.RegisterReadme[*v1alpha1.Agent](api, cfg, newObj, func(obj *v1alpha1.Agent) *v1alpha1.Readme {
		return obj.Spec.Readme
	})
	resource.Register[*v1alpha1.Agent](api, cfg, newObj)

	// Direct Store.Upsert bypasses the authorizer for seeding — we
	// only want to test the readme path's gate, not whether PUT
	// itself respects authz (covered elsewhere).
	specJSON, err := json.Marshal(v1alpha1.AgentSpec{
		Title:  "Secret",
		Readme: &v1alpha1.Readme{ContentType: "text/markdown", Content: "internal-only"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(t.Context(), "default", "secret", "v1", specJSON, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	publicSpecJSON, err := json.Marshal(v1alpha1.AgentSpec{
		Title:  "Public",
		Readme: &v1alpha1.Readme{ContentType: "text/markdown", Content: "public docs"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(t.Context(), "default", "public", "v1", publicSpecJSON, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	// Latest readme — denied row 403.
	resp := api.Get("/v0/agents/secret/readme")
	require.Equal(t, http.StatusForbidden, resp.Code,
		"latest readme must respect Authorize: %s", resp.Body.String())

	// Versioned readme — denied row 403.
	resp = api.Get("/v0/agents/secret/versions/v1/readme")
	require.Equal(t, http.StatusForbidden, resp.Code,
		"versioned readme must respect Authorize: %s", resp.Body.String())

	// Allowed row still served.
	resp = api.Get("/v0/agents/public/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var got v1alpha1.Readme
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Equal(t, "public docs", got.Content)
}

// TestRegisterReadme_LegacyServerReadmeRespectsAuthorize pins the
// same invariant for the legacy `/v0/servers/{name}/readme` path. The
// legacy alias predates the generic readme subresource and bypasses
// the regular routing, so it carries its own authz gate.
func TestRegisterReadme_LegacyServerReadmeRespectsAuthorize(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.mcpservers")

	_, api := humatest.New(t)
	cfg := resource.Config{
		Kind:       v1alpha1.KindMCPServer,
		BasePrefix: "/v0",
		Store:      store,
		Authorize: func(ctx context.Context, in resource.AuthorizeInput) error {
			if in.Verb == "get" && in.Name == "secret-server" {
				return huma.Error403Forbidden("denied")
			}
			return nil
		},
	}
	resource.RegisterLegacyServerReadme(api, cfg)

	specJSON, err := json.Marshal(v1alpha1.MCPServerSpec{
		Title:  "Secret",
		Readme: &v1alpha1.Readme{ContentType: "text/markdown", Content: "internal"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(t.Context(), v1alpha1.DefaultNamespace, "secret-server", "v1", specJSON, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	publicSpecJSON, err := json.Marshal(v1alpha1.MCPServerSpec{
		Title:  "Public",
		Readme: &v1alpha1.Readme{ContentType: "text/markdown", Content: "public"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(t.Context(), v1alpha1.DefaultNamespace, "public-server", "v1", publicSpecJSON, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	resp := api.Get("/v0/servers/secret-server/readme")
	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())

	resp = api.Get("/v0/servers/secret-server/versions/v1/readme")
	require.Equal(t, http.StatusForbidden, resp.Code, resp.Body.String())

	resp = api.Get("/v0/servers/public-server/readme")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
}

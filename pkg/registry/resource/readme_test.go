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

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// TestResourceRegister_AgentReadmeRoutesAndListProjection pins the
// auto-registered readme subresource: when the kind's typed envelope
// implements v1alpha1.ObjectWithReadme (Agent / MCPServer / Skill /
// Prompt do; Provider / Deployment do not), Register wires
// `/{plural}/{name}/readme` and the version-pinned variant inline.
// Callers don't pass a separate readme accessor.
func TestResourceRegister_AgentReadmeRoutesAndListProjection(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	_, api := humatest.New(t)
	resource.Register(api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

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

// TestRegister_NoReadmeRoutesForReadmeLessKinds pins that
// Provider / Deployment — which do NOT implement ObjectWithReadme —
// have no readme routes registered. A GET against the readme path 404s
// from Huma's router (no operation registered) rather than reaching the
// handler.
func TestRegister_NoReadmeRoutesForReadmeLessKinds(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, v1alpha1store.TableFor[v1alpha1.KindProvider])

	_, api := humatest.New(t)
	resource.Register(api, resource.Config{
		Kind:       v1alpha1.KindProvider,
		BasePrefix: "/v0",
		Store:      store,
	}, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })

	// Seed a row so the absence of a readme route is the failure mode,
	// not "row missing".
	specJSON, err := json.Marshal(v1alpha1.ProviderSpec{Platform: "test"})
	require.NoError(t, err)
	_, err = store.Upsert(t.Context(), v1alpha1.DefaultNamespace, "p1", "v1", specJSON, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	resp := api.Get("/v0/providers/p1/readme")
	require.Equal(t, http.StatusNotFound, resp.Code, resp.Body.String())
}

// TestRegisterReadme_RespectsAuthorize pins the row-level RBAC
// invariant for the readme subresource: a deny on (Kind, Name) at the
// regular GET handler MUST also block the readme path. Without this
// gate, an enterprise tenant could read README content (markdown body
// frequently containing setup instructions, internal hostnames,
// contact info) for resources they don't have grants for.
func TestRegisterReadme_RespectsAuthorize(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
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
	resource.Register(api, cfg, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })

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

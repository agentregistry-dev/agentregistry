//go:build integration

package resource_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// registerAgent wires the generic resource handler for *v1alpha1.Agent onto
// the given Huma API, against the supplied Store. It's a test-local helper
// so we don't pull the full registry_app into these tests.
func registerAgent(api huma.API, store *database.Store) {
	resource.Register[*v1alpha1.Agent](api, resource.Config{
		Kind:       v1alpha1.KindAgent,
		PathPrefix: "/v0/agents",
		Store:      store,
	}, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
}

// newTestPool is defined in database/store_v1alpha1_testutil_test.go (package
// database). Pulling into this package via a go test-only wrapper would
// duplicate too much; instead we expose a helper through a blank-import-free
// shim.
//
// The test table "agents" is seeded once per Pool by newV1Alpha1TestPool,
// which skips the test when Postgres isn't available on localhost.
func TestResourceRegister_AgentCRUD(t *testing.T) {
	t.Helper()

	pool := database.NewV1Alpha1TestPool(t)
	store := database.NewStore(pool, "agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// PUT a new agent.
	putBody := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{
			Name:    "alice",
			Version: "v1.0.0",
			Labels:  map[string]string{"team": "platform"},
		},
		Spec: v1alpha1.AgentSpec{
			Title: "Alice",
			Image: "ghcr.io/example/alice:1.0.0",
		},
	}
	resp := api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var gotAgent v1alpha1.Agent
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.Equal(t, v1alpha1.GroupVersion, gotAgent.APIVersion)
	require.Equal(t, v1alpha1.KindAgent, gotAgent.Kind)
	require.Equal(t, "alice", gotAgent.Metadata.Name)
	require.Equal(t, "v1.0.0", gotAgent.Metadata.Version)
	require.EqualValues(t, 1, gotAgent.Metadata.Generation)
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

	// LIST with label selector.
	resp = api.Get("/v0/agents?labels=team%3Dplatform")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var list struct {
		Items      []v1alpha1.Agent `json:"items"`
		NextCursor string           `json:"nextCursor,omitempty"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &list))
	require.Len(t, list.Items, 1)
	require.Equal(t, "alice", list.Items[0].Metadata.Name)

	// PUT again with same spec — generation must stay at 1.
	resp = api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.EqualValues(t, 1, gotAgent.Metadata.Generation, "no-op apply preserves generation")

	// PUT with mutated spec — generation bumps to 2.
	putBody.Spec.Title = "Alice v2"
	resp = api.Put("/v0/agents/alice/v1.0.0", putBody)
	require.Equal(t, http.StatusOK, resp.Code)
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &gotAgent))
	require.EqualValues(t, 2, gotAgent.Metadata.Generation)
	require.Equal(t, "Alice v2", gotAgent.Spec.Title)

	// DELETE.
	resp = api.Delete("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusNoContent, resp.Code)

	// GET after delete → 404.
	resp = api.Get("/v0/agents/alice/v1.0.0")
	require.Equal(t, http.StatusNotFound, resp.Code)
}

func TestResourceRegister_AgentWrongKindRejected(t *testing.T) {
	t.Helper()

	pool := database.NewV1Alpha1TestPool(t)
	store := database.NewStore(pool, "agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	// Body carries Kind: "Skill" but PUT targets the agents handler.
	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: "Skill"},
		Metadata: v1alpha1.ObjectMeta{Name: "bob", Version: "v1"},
		Spec:     v1alpha1.AgentSpec{Title: "wrong kind"},
	}
	resp := api.Put("/v0/agents/bob/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
}

func TestResourceRegister_AgentPathMismatchRejected(t *testing.T) {
	t.Helper()
	pool := database.NewV1Alpha1TestPool(t)
	store := database.NewStore(pool, "agents")

	_, api := humatest.New(t)
	registerAgent(api, store)

	body := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Name: "mismatched", Version: "v1"},
	}
	resp := api.Put("/v0/agents/alice/v1", body)
	require.Equal(t, http.StatusBadRequest, resp.Code, fmt.Sprintf("body=%s", resp.Body.String()))
}

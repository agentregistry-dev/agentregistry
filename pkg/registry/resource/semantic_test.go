//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/semantic"
)

// zeroPadVec returns a fixed-1536-dim vector with the first positions
// set from values. Shared with the Store embedding tests.
func zeroPadVec(values ...float32) []float32 {
	v := make([]float32, 1536)
	copy(v, values)
	return v
}

func TestSemanticSearch_ListEndpointRanksByDistance(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	// Seed three agents with orthogonal embeddings so cosine distance
	// is deterministic: queryVector=[1,0,0,...]
	//   near      -> distance 0
	//   farther   -> distance 1
	//   farthest  -> distance 2
	mkAgent := func(name string, vec []float32) {
		spec, err := json.Marshal(v1alpha1.AgentSpec{Title: name})
		require.NoError(t, err)
		_, err = agents.Upsert(ctx, "default", name, "v1", spec, v1alpha1store.UpsertOpts{})
		require.NoError(t, err)
		require.NoError(t, agents.SetEmbedding(ctx, "default", name, "v1", semantic.SemanticEmbedding{
			Vector:     vec,
			Provider:   "fake",
			Dimensions: 1536,
		}))
	}
	mkAgent("near", zeroPadVec(1, 0, 0))
	mkAgent("farther", zeroPadVec(0, 1, 0))
	mkAgent("farthest", zeroPadVec(-1, 0, 0))

	// SemanticSearchFunc always returns the same query vector.
	search := func(ctx context.Context, q string) ([]float32, error) {
		return zeroPadVec(1, 0, 0), nil
	}

	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}
	_, api := humatest.New(t)
	crud.Register(api, "/v0", stores, nil, nil, search, crud.PerKindHooks{})

	resp := api.Get("/v0/agents?semantic=anything")
	require.Equal(t, 200, resp.Code, resp.Body.String())

	var body struct {
		Items []struct {
			Metadata v1alpha1.ObjectMeta `json:"metadata"`
		} `json:"items"`
		SemanticScores []float32 `json:"semanticScores"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Len(t, body.Items, 3)
	require.Equal(t, "near", body.Items[0].Metadata.Name)
	require.Equal(t, "farther", body.Items[1].Metadata.Name)
	require.Equal(t, "farthest", body.Items[2].Metadata.Name)

	require.Len(t, body.SemanticScores, 3)
	require.InDelta(t, 0.0, body.SemanticScores[0], 1e-4)
	require.InDelta(t, 1.0, body.SemanticScores[1], 1e-4)
	require.InDelta(t, 2.0, body.SemanticScores[2], 1e-4)
}

// TestSemanticSearch_RespectsListFilterDenyList pins the row-level
// authz invariant for the `?semantic=` ranking path: even when a
// requesting user is allowed to call the list endpoint (Authorize
// gate passes), rows the user has been denied access to MUST NOT
// appear in the ranked results. The seam is the same one
// runList uses — Config.ListFilter returns an ExtraWhere fragment
// + ExtraArgs, which the Store rebases into the SemanticList SQL.
//
// Without this seam the score itself would leak existence and
// similarity to the query for denied rows. Regression-pin commit
// added to runSemanticList ListFilter wiring.
func TestSemanticSearch_RespectsListFilterDenyList(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	mkAgent := func(name string, vec []float32) {
		spec, err := json.Marshal(v1alpha1.AgentSpec{Title: name})
		require.NoError(t, err)
		_, err = agents.Upsert(ctx, "default", name, "v1", spec, v1alpha1store.UpsertOpts{})
		require.NoError(t, err)
		require.NoError(t, agents.SetEmbedding(ctx, "default", name, "v1", semantic.SemanticEmbedding{
			Vector:     vec,
			Provider:   "fake",
			Dimensions: 1536,
		}))
	}
	mkAgent("public", zeroPadVec(1, 0, 0))
	mkAgent("secret", zeroPadVec(0.95, 0.05, 0))
	mkAgent("public-far", zeroPadVec(-1, 0, 0))

	search := func(ctx context.Context, q string) ([]float32, error) {
		return zeroPadVec(1, 0, 0), nil
	}

	// ListFilter excludes the "secret" row — same shape an enterprise
	// per-role deny-list produces.
	listFilters := map[string]func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error){
		v1alpha1.KindAgent: func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error) {
			return "name <> $1", []any{"secret"}, nil
		},
	}

	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}
	_, api := humatest.New(t)
	crud.Register(api, "/v0", stores, nil, nil, search, crud.PerKindHooks{
		ListFilters: listFilters,
	})

	resp := api.Get("/v0/agents?semantic=anything")
	require.Equal(t, 200, resp.Code, resp.Body.String())

	var body struct {
		Items []struct {
			Metadata v1alpha1.ObjectMeta `json:"metadata"`
		} `json:"items"`
		SemanticScores []float32 `json:"semanticScores"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))

	names := make([]string, 0, len(body.Items))
	for _, item := range body.Items {
		names = append(names, item.Metadata.Name)
	}
	require.NotContains(t, names, "secret", "denied row leaked into semantic results")
	require.ElementsMatch(t, []string{"public", "public-far"}, names)
	require.Len(t, body.SemanticScores, len(body.Items),
		"score leakage check: scores must match item count, no orphan score for denied row")
}

// TestSemanticSearch_ListFilterScopeNoneReturnsEmpty pins the
// "deny everything" predicate (`1=0`) flowing into ?semantic= the
// same way it flows into the regular list. Mirrors the
// RoleProvider ScopeNone path enterprise emits when a principal has
// no permissions for a kind.
func TestSemanticSearch_ListFilterScopeNoneReturnsEmpty(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	spec, err := json.Marshal(v1alpha1.AgentSpec{Title: "anything"})
	require.NoError(t, err)
	_, err = agents.Upsert(ctx, "default", "anything", "v1", spec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)
	require.NoError(t, agents.SetEmbedding(ctx, "default", "anything", "v1", semantic.SemanticEmbedding{
		Vector:     zeroPadVec(1, 0, 0),
		Provider:   "fake",
		Dimensions: 1536,
	}))

	search := func(ctx context.Context, q string) ([]float32, error) {
		return zeroPadVec(1, 0, 0), nil
	}

	listFilters := map[string]func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error){
		v1alpha1.KindAgent: func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error) {
			return "1=0", nil, nil
		},
	}

	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}
	_, api := humatest.New(t)
	crud.Register(api, "/v0", stores, nil, nil, search, crud.PerKindHooks{
		ListFilters: listFilters,
	})

	resp := api.Get("/v0/agents?semantic=anything")
	require.Equal(t, 200, resp.Code, resp.Body.String())

	var body struct {
		Items []struct {
			Metadata v1alpha1.ObjectMeta `json:"metadata"`
		} `json:"items"`
		SemanticScores []float32 `json:"semanticScores"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Empty(t, body.Items, "ScopeNone (1=0) must elide all rows from semantic ranking")
	require.Empty(t, body.SemanticScores)
}

func TestSemanticSearch_ListReturns400WhenDisabled(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}
	_, api := humatest.New(t)
	// SemanticSearch = nil ⇒ `?semantic=` endpoint surface returns 400.
	crud.Register(api, "/v0", stores, nil, nil, nil, crud.PerKindHooks{})

	resp := api.Get("/v0/agents?semantic=anything")
	require.Equal(t, 400, resp.Code)
}

//go:build integration

package resource_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/builtins"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/semantic"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// zeroPadVec returns a fixed-1536-dim vector with the first positions
// set from values. Shared with the Store embedding tests.
func zeroPadVec(values ...float32) []float32 {
	v := make([]float32, 1536)
	copy(v, values)
	return v
}

func TestSemanticSearch_ListEndpointRanksByDistance(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
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
	builtins.RegisterBuiltins(api, "/v0", stores, nil, nil, nil, builtins.DeploymentHooks{}, search)

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

func TestSemanticSearch_ListReturns400WhenDisabled(t *testing.T) {
	pool := v1alpha1store.NewV1Alpha1TestPool(t)
	agents := v1alpha1store.NewStore(pool, "v1alpha1.agents")

	stores := map[string]*v1alpha1store.Store{v1alpha1.KindAgent: agents}
	_, api := humatest.New(t)
	// SemanticSearch = nil ⇒ `?semantic=` endpoint surface returns 400.
	builtins.RegisterBuiltins(api, "/v0", stores, nil, nil, nil, builtins.DeploymentHooks{}, nil)

	resp := api.Get("/v0/agents?semantic=anything")
	require.Equal(t, 400, resp.Code)
}

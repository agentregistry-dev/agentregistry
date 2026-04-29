//go:build integration

package seed

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestImportBuiltinSeedData_Populates(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	ctx := context.Background()

	require.NoError(t, ImportBuiltinSeedData(ctx, pool))

	store := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")

	// Cross-namespace list should surface the seeded rows. 35k lines of
	// seed.json → hundreds of rows.
	rows, _, err := store.List(ctx, v1alpha1store.ListOpts{})
	require.NoError(t, err)
	require.Greater(t, len(rows), 10, "expected seed import to populate many MCPServer rows")

	// Every seeded row should carry the seed label so ops can filter.
	withLabel, _, err := store.List(ctx, v1alpha1store.ListOpts{
		LabelSelector: map[string]string{"agentregistry.solo.io/seed": "builtin"},
	})
	require.NoError(t, err)
	require.Equal(t, len(rows), len(withLabel))
}

func TestImportBuiltinSeedData_Idempotent(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	ctx := context.Background()

	require.NoError(t, ImportBuiltinSeedData(ctx, pool))

	store := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")
	rows, _, err := store.List(ctx, v1alpha1store.ListOpts{Limit: 1000})
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	// Generation on first seeded row.
	var sample *v1alpha1.RawObject
	for _, r := range rows {
		if r.Metadata.Name != "" {
			sample = r
			break
		}
	}
	require.NotNil(t, sample)
	gen := sample.Metadata.Generation

	// Re-seed — generation must not change (spec bytes unchanged ⇒ no bump).
	require.NoError(t, ImportBuiltinSeedData(ctx, pool))

	reread, err := store.Get(ctx, sample.Metadata.Namespace, sample.Metadata.Name, sample.Metadata.Version)
	require.NoError(t, err)
	require.Equal(t, gen, reread.Metadata.Generation, "re-seed must not bump generation")
}

func TestImportBuiltinSeedData_SpecStructure(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	ctx := context.Background()

	require.NoError(t, ImportBuiltinSeedData(ctx, pool))

	store := v1alpha1store.NewStore(pool, "v1alpha1.mcp_servers")
	rows, _, err := store.List(ctx, v1alpha1store.ListOpts{Limit: 500})
	require.NoError(t, err)
	require.NotEmpty(t, rows)

	// At least one row should have a non-empty description; confirms the
	// translation carried over upstream fields.
	var withDescription int
	for _, r := range rows {
		var spec v1alpha1.MCPServerSpec
		require.NoError(t, json.Unmarshal(r.Spec, &spec))
		if spec.Description != "" {
			withDescription++
		}
	}
	require.Greater(t, withDescription, 0, "expected at least one seeded MCPServer with a description")
}

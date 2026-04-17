//go:build integration

package database

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

const testTable = "agents"

func mustSpec(t *testing.T, spec any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(spec)
	require.NoError(t, err)
	return b
}

func TestV1Alpha1Store_UpsertCreatesRow(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	res, err := store.Upsert(ctx, "foo", "v1.0.0", spec, nil)
	require.NoError(t, err)
	require.True(t, res.Created)
	require.True(t, res.SpecChanged)
	require.EqualValues(t, 1, res.Generation)

	obj, err := store.Get(ctx, "foo", "v1.0.0")
	require.NoError(t, err)
	require.Equal(t, "foo", obj.Metadata.Name)
	require.Equal(t, "v1.0.0", obj.Metadata.Version)
	require.EqualValues(t, 1, obj.Metadata.Generation)
	require.False(t, obj.Metadata.CreatedAt.IsZero())
}

func TestV1Alpha1Store_UpsertNoOpPreservesGeneration(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	_, err := store.Upsert(ctx, "foo", "v1", spec, nil)
	require.NoError(t, err)

	// Re-apply identical spec: generation must not bump.
	res, err := store.Upsert(ctx, "foo", "v1", spec, nil)
	require.NoError(t, err)
	require.False(t, res.Created)
	require.False(t, res.SpecChanged)
	require.EqualValues(t, 1, res.Generation)
}

func TestV1Alpha1Store_UpsertBumpsGenerationOnSpecChange(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec1 := mustSpec(t, v1alpha1.AgentSpec{Title: "first"})
	spec2 := mustSpec(t, v1alpha1.AgentSpec{Title: "second"})

	_, err := store.Upsert(ctx, "foo", "v1", spec1, nil)
	require.NoError(t, err)

	res, err := store.Upsert(ctx, "foo", "v1", spec2, nil)
	require.NoError(t, err)
	require.True(t, res.SpecChanged)
	require.EqualValues(t, 2, res.Generation)

	obj, err := store.Get(ctx, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 2, obj.Metadata.Generation)
}

func TestV1Alpha1Store_LatestVersionSemverToggle(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"v1.0.0", "v1.2.0", "v0.9.0", "v2.0.0", "v1.10.1"} {
		_, err := store.Upsert(ctx, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), nil)
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", latest.Metadata.Version, "v2.0.0 is highest semver")

	// Per-row is_latest_version is a derived column — Get returns the raw row
	// and the metadata we expose doesn't carry that bit, but GetLatest works
	// off it, so a successful GetLatest with the correct version is proof
	// enough that the partial unique index picked the right winner.
}

func TestV1Alpha1Store_LatestVersionFallbackOnInvalidSemver(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Non-semver versions: expect most-recently-updated to win.
	for _, v := range []string{"alpha", "beta", "gamma"} {
		_, err := store.Upsert(ctx, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), nil)
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, "gamma", latest.Metadata.Version, "last-upserted non-semver wins")
}

func TestV1Alpha1Store_PatchStatusDisjointFromSpec(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	_, err := store.Upsert(ctx, "foo", "v1", spec, nil)
	require.NoError(t, err)

	// Patch status with a Ready condition.
	err = store.PatchStatus(ctx, "foo", "v1", func(s *v1alpha1.Status) {
		s.ObservedGeneration = 1
		s.SetCondition(v1alpha1.Condition{
			Type:   "Ready",
			Status: v1alpha1.ConditionTrue,
			Reason: "Converged",
		})
	})
	require.NoError(t, err)

	obj, err := store.Get(ctx, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 1, obj.Metadata.Generation, "generation must not change on status patch")
	require.EqualValues(t, 1, obj.Status.ObservedGeneration)
	require.Len(t, obj.Status.Conditions, 1)
	require.Equal(t, "Ready", obj.Status.Conditions[0].Type)
	require.Equal(t, v1alpha1.ConditionTrue, obj.Status.Conditions[0].Status)
}

func TestV1Alpha1Store_PatchStatusNotFound(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.PatchStatus(ctx, "nope", "v1", func(s *v1alpha1.Status) {})
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestV1Alpha1Store_GetNotFound(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Get(ctx, "nope", "v1")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))

	_, err = store.GetLatest(ctx, "nope")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

func TestV1Alpha1Store_Delete(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), nil)
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "foo", "v2", mustSpec(t, v1alpha1.AgentSpec{}), nil)
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, "foo", "v2"))

	// After deleting v2, v1 must be promoted to latest.
	latest, err := store.GetLatest(ctx, "foo")
	require.NoError(t, err)
	require.Equal(t, "v1", latest.Metadata.Version)

	// Non-existent delete returns ErrNotFound.
	err = store.Delete(ctx, "foo", "v99")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestV1Alpha1Store_List(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, "a", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "A"}), map[string]string{"owner": "x"})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "b", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "B"}), map[string]string{"owner": "y"})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "c", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "C"}), map[string]string{"owner": "x"})
	require.NoError(t, err)

	// No filter → all rows.
	all, _, err := store.List(ctx, ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 3)

	// Label filter.
	filtered, _, err := store.List(ctx, ListOpts{LabelSelector: map[string]string{"owner": "x"}})
	require.NoError(t, err)
	require.Len(t, filtered, 2)
	for _, r := range filtered {
		require.Equal(t, "x", r.Metadata.Labels["owner"])
	}

	// LatestOnly returns one row per name (each has only v1 here, so all 3).
	latest, _, err := store.List(ctx, ListOpts{LatestOnly: true})
	require.NoError(t, err)
	require.Len(t, latest, 3)
}

func TestV1Alpha1Store_FindReferrers(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	agents := NewStore(pool, "agents")
	ctx := context.Background()

	_, err := agents.Upsert(ctx, "refs-bar", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "bar", Version: "v1"}},
		}), nil)
	require.NoError(t, err)

	_, err = agents.Upsert(ctx, "refs-baz", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "baz", Version: "v1"}},
		}), nil)
	require.NoError(t, err)

	// Look up every agent that references mcp "bar@v1".
	pattern, err := json.Marshal(map[string]any{
		"mcpServers": []map[string]string{{"name": "bar", "version": "v1"}},
	})
	require.NoError(t, err)

	results, err := agents.FindReferrers(ctx, pattern, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "refs-bar", results[0].Metadata.Name)
}

func TestV1Alpha1Store_SeededProviders(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	providers := NewStore(pool, "providers")
	ctx := context.Background()

	local, err := providers.GetLatest(ctx, "local")
	require.NoError(t, err)
	require.Equal(t, "v1", local.Metadata.Version)

	var spec v1alpha1.ProviderSpec
	require.NoError(t, json.Unmarshal(local.Spec, &spec))
	require.Equal(t, v1alpha1.PlatformLocal, spec.Platform)

	k8s, err := providers.GetLatest(ctx, "kubernetes-default")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(k8s.Spec, &spec))
	require.Equal(t, v1alpha1.PlatformKubernetes, spec.Platform)
}

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

const testTable = "v1alpha1.agents"
const testNS = "default"

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
	res, err := store.Upsert(ctx, testNS, "foo", "v1.0.0", spec, nil, UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.True(t, res.SpecChanged)
	require.EqualValues(t, 1, res.Generation)

	obj, err := store.Get(ctx, testNS, "foo", "v1.0.0")
	require.NoError(t, err)
	require.Equal(t, testNS, obj.Metadata.Namespace)
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
	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec, nil, UpsertOpts{})
	require.NoError(t, err)

	res, err := store.Upsert(ctx, testNS, "foo", "v1", spec, nil, UpsertOpts{})
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

	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec1, nil, UpsertOpts{})
	require.NoError(t, err)

	res, err := store.Upsert(ctx, testNS, "foo", "v1", spec2, nil, UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.SpecChanged)
	require.EqualValues(t, 2, res.Generation)

	obj, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 2, obj.Metadata.Generation)
}

func TestV1Alpha1Store_LatestVersionSemverToggle(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"v1.0.0", "v1.2.0", "v0.9.0", "v2.0.0", "v1.10.1"} {
		_, err := store.Upsert(ctx, testNS, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), nil, UpsertOpts{})
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", latest.Metadata.Version, "v2.0.0 is highest semver")
}

func TestV1Alpha1Store_LatestVersionFallbackOnInvalidSemver(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"alpha", "beta", "gamma"} {
		_, err := store.Upsert(ctx, testNS, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), nil, UpsertOpts{})
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "gamma", latest.Metadata.Version, "last-upserted non-semver wins")
}

func TestV1Alpha1Store_PatchStatusDisjointFromSpec(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec, nil, UpsertOpts{})
	require.NoError(t, err)

	err = store.PatchStatus(ctx, testNS, "foo", "v1", func(s *v1alpha1.Status) {
		s.ObservedGeneration = 1
		s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Converged"})
	})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 1, obj.Metadata.Generation)
	require.EqualValues(t, 1, obj.Status.ObservedGeneration)
	require.Len(t, obj.Status.Conditions, 1)
	require.Equal(t, "Ready", obj.Status.Conditions[0].Type)
	require.Equal(t, v1alpha1.ConditionTrue, obj.Status.Conditions[0].Status)
}

func TestV1Alpha1Store_PatchStatusNotFound(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.PatchStatus(ctx, testNS, "nope", "v1", func(s *v1alpha1.Status) {})
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestV1Alpha1Store_GetNotFound(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Get(ctx, testNS, "nope", "v1")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))

	_, err = store.GetLatest(ctx, testNS, "nope")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

func TestV1Alpha1Store_DeleteSoftAndPromoteLatest(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), nil, UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, testNS, "foo", "v2", mustSpec(t, v1alpha1.AgentSpec{}), nil, UpsertOpts{})
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, testNS, "foo", "v2"))

	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "v1", latest.Metadata.Version)

	v2, err := store.Get(ctx, testNS, "foo", "v2")
	require.NoError(t, err)
	require.NotNil(t, v2.Metadata.DeletionTimestamp)

	err = store.Delete(ctx, testNS, "foo", "v99")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)

	require.NoError(t, store.Delete(ctx, testNS, "foo", "v2"))
}

func TestV1Alpha1Store_FinalizerGC(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "fin", "v1", mustSpec(t, v1alpha1.AgentSpec{}), nil,
		UpsertOpts{Finalizers: []string{"cleanup.example/thing"}})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "fin", "v1")
	require.NoError(t, err)
	require.Equal(t, []string{"cleanup.example/thing"}, obj.Metadata.Finalizers)

	require.NoError(t, store.Delete(ctx, testNS, "fin", "v1"))

	obj, err = store.Get(ctx, testNS, "fin", "v1")
	require.NoError(t, err)
	require.NotNil(t, obj.Metadata.DeletionTimestamp)
	require.Len(t, obj.Metadata.Finalizers, 1)

	n, err := store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, n)

	err = store.PatchFinalizers(ctx, testNS, "fin", "v1", func(f []string) []string { return nil })
	require.NoError(t, err)

	n, err = store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	_, err = store.Get(ctx, testNS, "fin", "v1")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestV1Alpha1Store_List(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, "team-a", "a", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "A"}), map[string]string{"owner": "x"}, UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "team-a", "b", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "B"}), map[string]string{"owner": "y"}, UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "team-b", "c", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "C"}), map[string]string{"owner": "x"}, UpsertOpts{})
	require.NoError(t, err)

	all, _, err := store.List(ctx, ListOpts{})
	require.NoError(t, err)
	require.Len(t, all, 3)

	teamA, _, err := store.List(ctx, ListOpts{Namespace: "team-a"})
	require.NoError(t, err)
	require.Len(t, teamA, 2)

	ownerX, _, err := store.List(ctx, ListOpts{LabelSelector: map[string]string{"owner": "x"}})
	require.NoError(t, err)
	require.Len(t, ownerX, 2)

	teamAOwnerX, _, err := store.List(ctx, ListOpts{Namespace: "team-a", LabelSelector: map[string]string{"owner": "x"}})
	require.NoError(t, err)
	require.Len(t, teamAOwnerX, 1)

	require.NoError(t, store.Delete(ctx, "team-a", "a", "v1"))

	alive, _, err := store.List(ctx, ListOpts{})
	require.NoError(t, err)
	require.Len(t, alive, 2)

	withTerm, _, err := store.List(ctx, ListOpts{IncludeTerminating: true})
	require.NoError(t, err)
	require.Len(t, withTerm, 3)
}

func TestV1Alpha1Store_ListExtraWhereRebasesPlaceholders(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		_, err := store.Upsert(ctx, "team-a", name, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: name}), nil, UpsertOpts{})
		require.NoError(t, err)
	}

	page1, nextCursor, err := store.List(ctx, ListOpts{
		Namespace:  "team-a",
		Limit:      1,
		ExtraWhere: "name <> $1",
		ExtraArgs:  []any{"b"},
	})
	require.NoError(t, err)
	require.Len(t, page1, 1)
	require.NotEmpty(t, nextCursor)
	require.NotEqual(t, "b", page1[0].Metadata.Name)

	page2, nextCursor2, err := store.List(ctx, ListOpts{
		Namespace:  "team-a",
		Limit:      1,
		Cursor:     nextCursor,
		ExtraWhere: "name <> $1",
		ExtraArgs:  []any{"b"},
	})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Empty(t, nextCursor2)
	require.NotEqual(t, "b", page2[0].Metadata.Name)
	require.NotEqual(t, page1[0].Metadata.Name, page2[0].Metadata.Name)
}

func TestV1Alpha1Store_ListCursorPagination(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		_, err := store.Upsert(ctx, testNS, name, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: name}), nil, UpsertOpts{})
		require.NoError(t, err)
	}

	page1, nextCursor, err := store.List(ctx, ListOpts{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, nextCursor)
	require.NotEqual(t, "more", nextCursor)

	page2, nextCursor2, err := store.List(ctx, ListOpts{Limit: 2, Cursor: nextCursor})
	require.NoError(t, err)
	require.Len(t, page2, 1)
	require.Empty(t, nextCursor2)

	seen := map[string]bool{}
	for _, obj := range append(page1, page2...) {
		require.False(t, seen[obj.Metadata.Name], "cursor pagination should not repeat rows")
		seen[obj.Metadata.Name] = true
	}
	require.Len(t, seen, 3)
}

func TestV1Alpha1Store_ListRejectsInvalidCursor(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)

	_, _, err := store.List(context.Background(), ListOpts{Cursor: "not-a-valid-cursor"})
	require.ErrorIs(t, err, ErrInvalidCursor)
}

func TestV1Alpha1Store_PatchAnnotationsPreservesExistingKeys(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "annotated", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "annotated"}), nil, UpsertOpts{
		Annotations: map[string]string{"keep": "me"},
	})
	require.NoError(t, err)

	err = store.PatchAnnotations(ctx, testNS, "annotated", "v1", func(annotations map[string]string) map[string]string {
		annotations["add"] = "value"
		return annotations
	})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "annotated", "v1")
	require.NoError(t, err)
	require.Equal(t, map[string]string{
		"add":  "value",
		"keep": "me",
	}, obj.Metadata.Annotations)
}

func TestV1Alpha1Store_FindReferrers(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	agents := NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "refs-bar", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "bar", Version: "v1"}},
		}), nil, UpsertOpts{})
	require.NoError(t, err)

	_, err = agents.Upsert(ctx, testNS, "refs-baz", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "baz", Version: "v1"}},
		}), nil, UpsertOpts{})
	require.NoError(t, err)

	pattern, err := json.Marshal(map[string]any{
		"mcpServers": []map[string]string{{"name": "bar", "version": "v1"}},
	})
	require.NoError(t, err)

	results, err := agents.FindReferrers(ctx, "", pattern, false)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "refs-bar", results[0].Metadata.Name)
}

func TestV1Alpha1Store_SeededProviders(t *testing.T) {
	pool := NewV1Alpha1TestPool(t)
	providers := NewStore(pool, "v1alpha1.providers")
	ctx := context.Background()

	local, err := providers.GetLatest(ctx, "default", "local")
	require.NoError(t, err)
	require.Equal(t, "v1", local.Metadata.Version)

	var spec v1alpha1.ProviderSpec
	require.NoError(t, json.Unmarshal(local.Spec, &spec))
	require.Equal(t, v1alpha1.PlatformLocal, spec.Platform)

	k8s, err := providers.GetLatest(ctx, "default", "kubernetes-default")
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(k8s.Spec, &spec))
	require.Equal(t, v1alpha1.PlatformKubernetes, spec.Platform)
}

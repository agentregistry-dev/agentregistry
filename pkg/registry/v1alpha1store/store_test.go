//go:build integration

package v1alpha1store

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// uuidPattern matches the canonical 8-4-4-4-12 textual UUID Postgres
// emits for `uid::text`. Sufficient for assertion: the column type is
// already UUID, so we only need to confirm the wire shape.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

const testTable = "v1alpha1.agents"
const testNS = "default"

func mustSpec(t *testing.T, spec any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(spec)
	require.NoError(t, err)
	return b
}

func TestStore_UpsertCreatesRow(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	res, err := store.Upsert(ctx, testNS, "foo", "v1.0.0", spec, UpsertOpts{})
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

func TestStore_UpsertNoOpPreservesGeneration(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec, UpsertOpts{})
	require.NoError(t, err)

	res, err := store.Upsert(ctx, testNS, "foo", "v1", spec, UpsertOpts{})
	require.NoError(t, err)
	require.False(t, res.Created)
	require.False(t, res.SpecChanged)
	require.EqualValues(t, 1, res.Generation)
}

func TestStore_UpsertBumpsGenerationOnSpecChange(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec1 := mustSpec(t, v1alpha1.AgentSpec{Title: "first"})
	spec2 := mustSpec(t, v1alpha1.AgentSpec{Title: "second"})

	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec1, UpsertOpts{})
	require.NoError(t, err)

	res, err := store.Upsert(ctx, testNS, "foo", "v1", spec2, UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.SpecChanged)
	require.EqualValues(t, 2, res.Generation)

	obj, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 2, obj.Metadata.Generation)
}

func TestStore_LatestVersionSemverToggle(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"v1.0.0", "v1.2.0", "v0.9.0", "v2.0.0", "v1.10.1"} {
		_, err := store.Upsert(ctx, testNS, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), UpsertOpts{})
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "v2.0.0", latest.Metadata.Version, "v2.0.0 is highest semver")
}

func TestStore_LatestVersionFallbackOnInvalidSemver(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, v := range []string{"alpha", "beta", "gamma"} {
		_, err := store.Upsert(ctx, testNS, "foo", v, mustSpec(t, v1alpha1.AgentSpec{Title: v}), UpsertOpts{})
		require.NoError(t, err)
	}

	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "gamma", latest.Metadata.Version, "last-upserted non-semver wins")
}

func TestStore_PatchStatusDisjointFromSpec(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	spec := mustSpec(t, v1alpha1.AgentSpec{Title: "alpha"})
	_, err := store.Upsert(ctx, testNS, "foo", "v1", spec, UpsertOpts{})
	require.NoError(t, err)

	// Store.PatchStatus now takes an opaque-bytes mutator; the typed
	// Status callback wraps through v1alpha1.StatusPatcher.
	err = store.PatchStatus(ctx, testNS, "foo", "v1", v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
		s.ObservedGeneration = 1
		s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Converged"})
	}))
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.EqualValues(t, 1, obj.Metadata.Generation)
	// obj.Status is raw bytes at the RawObject layer; decode to the
	// typed Status via the storage codec to inspect the fields.
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(obj.Status, &status))
	require.EqualValues(t, 1, status.ObservedGeneration)
	require.Len(t, status.Conditions, 1)
	require.Equal(t, "Ready", status.Conditions[0].Type)
	require.Equal(t, v1alpha1.ConditionTrue, status.Conditions[0].Status)
}

func TestStore_PatchStatusNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.PatchStatus(ctx, testNS, "nope", "v1", v1alpha1.StatusPatcher(func(*v1alpha1.Status) {}))
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_GetNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Get(ctx, testNS, "nope", "v1")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))

	_, err = store.GetLatest(ctx, testNS, "nope")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

// TestStore_DeleteFinalizerFreeHardDeletes pins the K8s-style fast
// path: a row with no finalizers hard-deletes synchronously rather
// than going through soft-delete + GC. Without this, "arctl delete X"
// followed by "arctl apply X" hits ErrTerminating until the (currently
// non-existent) periodic GC purges the row, blocking re-apply
// indefinitely. Reported by josh-pritchard on PR #455:
// "Soft-delete blocks re-apply for every v1alpha1 kind."
func TestStore_DeleteFinalizerFreeHardDeletes(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, testNS, "foo", "v2", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, testNS, "foo", "v2"))

	// is_latest_version recomputes off the surviving live rows.
	latest, err := store.GetLatest(ctx, testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, "v1", latest.Metadata.Version)

	// v2 is gone — not soft-deleted, fully removed.
	_, err = store.Get(ctx, testNS, "foo", "v2")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)

	// Re-apply with the same identity is a fresh row, generation 1.
	res, err := store.Upsert(ctx, testNS, "foo", "v2", mustSpec(t, v1alpha1.AgentSpec{Title: "reborn"}), UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.Created, "re-applied row must be a fresh create after hard-delete")
	require.EqualValues(t, 1, res.Generation)

	// Deleting a missing version still errors.
	err = store.Delete(ctx, testNS, "foo", "v99")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

// TestStore_DeleteWithFinalizersSoftDeletes pins the slower path:
// rows that carry finalizers go through the K8s soft-delete dance —
// deletion_timestamp set, row stays visible to Get, finalizer must
// drain before PurgeFinalized actually removes the row. Re-apply
// before the drain returns ErrTerminating.
func TestStore_DeleteWithFinalizersSoftDeletes(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)

	// Attach a finalizer out-of-band so Delete takes the soft-delete branch.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "foo", "v1",
		func([]string) []string { return []string{"finalizer.example.com"} }))

	require.NoError(t, store.Delete(ctx, testNS, "foo", "v1"))

	// Row stays visible — terminating, but not gone.
	row, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.NotNil(t, row.Metadata.DeletionTimestamp)

	// Re-apply against the terminating row is rejected.
	_, err = store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.ErrorIs(t, err, ErrTerminating)

	// Drain the finalizer + PurgeFinalized → row is gone, re-apply
	// becomes a fresh create.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "foo", "v1",
		func([]string) []string { return nil }))
	purged, err := store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.GreaterOrEqual(t, purged, int64(1))

	res, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.Created)
}

// readFinalizers reads the current finalizers slice for a row through
// PatchFinalizers's identity mutator. RawObject doesn't expose the
// finalizers column directly (there's no public Get accessor), and
// PatchFinalizers is the only typed read path that doesn't require a
// raw SQL query.
func readFinalizers(t *testing.T, ctx context.Context, store *Store, ns, name, version string) []string {
	t.Helper()
	var got []string
	require.NoError(t, store.PatchFinalizers(ctx, ns, name, version, func(current []string) []string {
		got = append([]string(nil), current...)
		return current
	}))
	return got
}

// TestStore_UpsertSeedsInitialFinalizers pins the atomic seam used by
// kinds whose teardown is owned by a reconciler: passing
// InitialFinalizers on the create path of Upsert writes the row with
// the finalizer in the same transaction as the spec, so a concurrent
// DELETE arriving before any out-of-band PatchFinalizers can't take
// the hard-delete fast path and skip the reconciler's cleanup.
func TestStore_UpsertSeedsInitialFinalizers(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	const finalizer = "finalizer.example.com/teardown"
	res, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{
		InitialFinalizers: []string{finalizer},
	})
	require.NoError(t, err)
	require.True(t, res.Created)

	// The freshly-written row carries the seeded finalizer.
	require.Equal(t, []string{finalizer}, readFinalizers(t, ctx, store, testNS, "foo", "v1"))

	// Delete now takes the soft-delete branch (because the row is
	// already finalized) — without InitialFinalizers it would hard-
	// delete and orphan whatever the reconciler was supposed to clean
	// up.
	require.NoError(t, store.Delete(ctx, testNS, "foo", "v1"))
	row, err := store.Get(ctx, testNS, "foo", "v1")
	require.NoError(t, err)
	require.NotNil(t, row.Metadata.DeletionTimestamp, "finalizer-seeded row must soft-delete, not hard-delete")
}

// TestStore_UpsertInitialFinalizersIgnoredOnUpdate pins the contract:
// InitialFinalizers seeds the row only on the create path. A subsequent
// Upsert against the same identity must not overwrite finalizers that
// were attached out-of-band (or seeded earlier), since updates are not
// the authoritative finalizer-mutation seam — PatchFinalizers is.
func TestStore_UpsertInitialFinalizersIgnoredOnUpdate(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	const seeded = "finalizer.example.com/teardown"
	_, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{
		InitialFinalizers: []string{seeded},
	})
	require.NoError(t, err)

	// Re-apply with a different InitialFinalizers slice + a spec change
	// to force the update branch. Existing finalizers must be preserved
	// untouched.
	res, err := store.Upsert(ctx, testNS, "foo", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "updated"}), UpsertOpts{
		InitialFinalizers: []string{"different.example.com/never-applied"},
	})
	require.NoError(t, err)
	require.False(t, res.Created)
	require.True(t, res.SpecChanged)

	require.Equal(t, []string{seeded},
		readFinalizers(t, ctx, store, testNS, "foo", "v1"),
		"update path must not overwrite finalizers; PatchFinalizers is the only mutation seam")
}

// TestStore_UpsertRejectsTerminatingRow guards the Kubernetes-style
// invariant: once a row is soft-deleted, it cannot be mutated in place via
// Upsert. Pre-fix behavior was a silent partial-update (ON CONFLICT bumped
// generation + spec but left deletion_timestamp set, so the row stayed
// invisible to GetLatest). Now Upsert returns ErrTerminating and the caller
// must drain finalizers + purge + re-apply to get a live row.
func TestStore_UpsertRejectsTerminatingRow(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "term", "v1",
		mustSpec(t, v1alpha1.AgentSpec{}),
		UpsertOpts{})
	require.NoError(t, err)

	// Attach a finalizer out-of-band so soft-delete leaves the row pending
	// drain. UpsertOpts has no Finalizers field anymore — that's the
	// orphan-reconciler internal seam, not a public API.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "term", "v1", func([]string) []string { return []string{"cleanup.example/thing"} }))

	// Soft-delete: deletion_timestamp is set but the row survives pending
	// finalizer drain.
	require.NoError(t, store.Delete(ctx, testNS, "term", "v1"))

	// Upsert against the terminating row must fail with ErrTerminating —
	// not silently half-update, and not masquerade as a successful create.
	_, err = store.Upsert(ctx, testNS, "term", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Description: "updated"}),
		UpsertOpts{})
	require.ErrorIs(t, err, ErrTerminating,
		"Upsert on a terminating row must reject with ErrTerminating")

	// Correct recovery path: drop the finalizer → GC purges → re-Upsert succeeds.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "term", "v1", func([]string) []string { return nil }))
	purged, err := store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, purged)

	res, err := store.Upsert(ctx, testNS, "term", "v1",
		mustSpec(t, v1alpha1.AgentSpec{Description: "fresh"}),
		UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.Created, "post-purge Upsert must be treated as a fresh create")
	require.EqualValues(t, 1, res.Generation, "generation must restart at 1 after purge, not continue from the terminating row")

	obj, err := store.GetLatest(ctx, testNS, "term")
	require.NoError(t, err)
	require.Equal(t, "v1", obj.Metadata.Version,
		"the resurrected row must be visible as latest")
}

func TestStore_FinalizerGC(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "fin", "v1", mustSpec(t, v1alpha1.AgentSpec{}), UpsertOpts{})
	require.NoError(t, err)

	// Attach a finalizer via PatchFinalizers (the only public path now —
	// UpsertOpts has no Finalizers field). PurgeFinalized respecting the
	// finalizer is the behavior under test.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "fin", "v1", func([]string) []string { return []string{"cleanup.example/thing"} }))

	require.NoError(t, store.Delete(ctx, testNS, "fin", "v1"))

	obj, err := store.Get(ctx, testNS, "fin", "v1")
	require.NoError(t, err)
	require.NotNil(t, obj.Metadata.DeletionTimestamp)

	// First purge pass: row is soft-deleted but finalizer is non-empty,
	// so PurgeFinalized must skip it.
	n, err := store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 0, n, "finalized purge must skip rows with non-empty finalizers")

	err = store.PatchFinalizers(ctx, testNS, "fin", "v1", func(f []string) []string { return nil })
	require.NoError(t, err)

	// Second purge pass: finalizers drained, row should now be hard-deleted.
	n, err = store.PurgeFinalized(ctx)
	require.NoError(t, err)
	require.EqualValues(t, 1, n)

	_, err = store.Get(ctx, testNS, "fin", "v1")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_List(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, "team-a", "a", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "A"}), UpsertOpts{Labels: map[string]string{"owner": "x"}})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "team-a", "b", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "B"}), UpsertOpts{Labels: map[string]string{"owner": "y"}})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, "team-b", "c", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "C"}), UpsertOpts{Labels: map[string]string{"owner": "x"}})
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

	// Attach a finalizer so the next Delete takes the soft-delete branch
	// rather than the finalizer-free fast-path (which hard-deletes).
	require.NoError(t, store.PatchFinalizers(ctx, "team-a", "a", "v1",
		func([]string) []string { return []string{"finalizer.example.com"} }))
	require.NoError(t, store.Delete(ctx, "team-a", "a", "v1"))

	alive, _, err := store.List(ctx, ListOpts{})
	require.NoError(t, err)
	require.Len(t, alive, 2)

	// IncludeTerminating exposes the soft-deleted row.
	withTerm, _, err := store.List(ctx, ListOpts{IncludeTerminating: true})
	require.NoError(t, err)
	require.Len(t, withTerm, 3)
}

func TestStore_ListLatestOnlyIncludeTerminating(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "foo", "v1.0.0", mustSpec(t, v1alpha1.AgentSpec{Title: "v1"}), UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, testNS, "foo", "v2.0.0", mustSpec(t, v1alpha1.AgentSpec{Title: "v2"}), UpsertOpts{})
	require.NoError(t, err)

	// Soft-delete the older version via the finalizer path so it goes
	// terminating and recomputeLatest clears is_latest_version on it.
	require.NoError(t, store.PatchFinalizers(ctx, testNS, "foo", "v1.0.0",
		func([]string) []string { return []string{"finalizer.example.com"} }))
	require.NoError(t, store.Delete(ctx, testNS, "foo", "v1.0.0"))

	// LatestOnly without IncludeTerminating: only the live latest (v2).
	live, _, err := store.List(ctx, ListOpts{LatestOnly: true})
	require.NoError(t, err)
	require.Len(t, live, 1)
	require.Equal(t, "v2.0.0", live[0].Metadata.Version)

	// LatestOnly + IncludeTerminating: live latest plus the terminating row.
	combined, _, err := store.List(ctx, ListOpts{LatestOnly: true, IncludeTerminating: true})
	require.NoError(t, err)
	require.Len(t, combined, 2)
	versions := []string{combined[0].Metadata.Version, combined[1].Metadata.Version}
	require.ElementsMatch(t, []string{"v1.0.0", "v2.0.0"}, versions)
}

func TestStore_ListExtraWhereRebasesPlaceholders(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		_, err := store.Upsert(ctx, "team-a", name, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: name}), UpsertOpts{})
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

// TestStore_ListExtraWhereRejectsMismatch verifies that the
// Store rejects ExtraWhere / ExtraArgs combinations whose placeholder
// count doesn't match the arg count, rather than silently executing a
// wrong query. Prevents SQL injection via accidental mis-parameterized
// fragments in the RBAC / authz surface.
func TestStore_ListExtraWhereRejectsMismatch(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	cases := []struct {
		name  string
		where string
		args  []any
	}{
		{"fragment uses $1 but no args supplied", "name = $1", nil},
		{"fragment uses $1 $2 but only one arg", "name = $1 AND version = $2", []any{"a"}},
		{"args supplied but fragment has no placeholder", "is_latest_version", []any{"a"}},
		{"fragment has two distinct but three args", "name = $1 AND version = $2", []any{"a", "b", "c"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := store.List(ctx, ListOpts{
				ExtraWhere: tc.where,
				ExtraArgs:  tc.args,
			})
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidExtraWhere)
		})
	}

	// Repeated use of the same placeholder counts once and is valid.
	_, _, err := store.List(ctx, ListOpts{
		ExtraWhere: "name = $1 OR version = $1",
		ExtraArgs:  []any{"x"},
	})
	require.NoError(t, err)
}

func TestStore_ListCursorPagination(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		_, err := store.Upsert(ctx, testNS, name, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: name}), UpsertOpts{})
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

func TestStore_ListRejectsInvalidCursor(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)

	_, _, err := store.List(context.Background(), ListOpts{Cursor: "not-a-valid-cursor"})
	require.ErrorIs(t, err, ErrInvalidCursor)
}

// TestStore_ListCursorStableUnderStatusChurn exercises the
// reason List orders by (namespace, name, version, updated_at) ASC
// rather than updated_at DESC: a row whose updated_at moves under a
// concurrent PatchStatus must not jump pages or get returned twice.
//
// Setup: 4 rows, page size 2 → first page returns rows 1+2.
// Mid-flight: row 1 (already returned on page 1) gets a status patch
// that bumps its updated_at past every other row's. With identity-first
// ordering, page 2 still returns rows 3+4 in order — the churned row
// is anchored by (namespace, name, version) which never changes.
func TestStore_ListCursorStableUnderStatusChurn(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	names := []string{"alpha", "beta", "gamma", "delta"} // lexical order: alpha, beta, delta, gamma
	for _, n := range names {
		_, err := store.Upsert(ctx, testNS, n, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: n}), UpsertOpts{})
		require.NoError(t, err)
	}

	page1, cursor, err := store.List(ctx, ListOpts{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, "alpha", page1[0].Metadata.Name)
	require.Equal(t, "beta", page1[1].Metadata.Name)

	// Bump the first row's updated_at via PatchStatus — under the old
	// updated_at-DESC ordering this row would float to the top of page 2
	// (returned twice) or knock another row off (page 2 misses a row).
	require.NoError(t, store.PatchStatus(ctx, testNS, "alpha", "v1", func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"observedGeneration":1}`), nil
	}))

	page2, cursor2, err := store.List(ctx, ListOpts{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Empty(t, cursor2)
	require.Len(t, page2, 2, "page2 must contain exactly the remaining rows")
	require.Equal(t, "delta", page2[0].Metadata.Name, "identity ordering puts delta before gamma")
	require.Equal(t, "gamma", page2[1].Metadata.Name)

	seen := map[string]int{}
	for _, obj := range append(page1, page2...) {
		seen[obj.Metadata.Name]++
	}
	for _, n := range names {
		require.Equal(t, 1, seen[n], "row %q must appear exactly once across pages", n)
	}
}

func TestStore_PatchAnnotationsPreservesExistingKeys(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, testNS, "annotated", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "annotated"}), UpsertOpts{
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

func TestStore_FindReferrers(t *testing.T) {
	pool := NewTestPool(t)
	agents := NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := agents.Upsert(ctx, testNS, "refs-bar", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "bar", Version: "v1"}},
		}), UpsertOpts{})
	require.NoError(t, err)

	_, err = agents.Upsert(ctx, testNS, "refs-baz", "v1",
		mustSpec(t, v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "baz", Version: "v1"}},
		}), UpsertOpts{})
	require.NoError(t, err)

	pattern, err := json.Marshal(map[string]any{
		"mcpServers": []map[string]string{{"name": "bar", "version": "v1"}},
	})
	require.NoError(t, err)

	results, err := agents.FindReferrers(ctx, pattern, FindReferrersOpts{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "refs-bar", results[0].Metadata.Name)
}

func TestStore_SeededProviders(t *testing.T) {
	pool := NewTestPool(t)
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

// TestStore_NotifyPayloadDiscreteFields guards the R2 fix:
// the status NOTIFY trigger emits (namespace, name, version) as three
// discrete JSON fields instead of a concatenated "ns/name/version"
// string. The previous shape was ambiguous when name contained `/`
// (nameRegex explicitly allows slashes for DNS-subdomain-style names
// like `ai.exa/exa`). The test uses such a name to confirm the parse
// survives round-trip unambiguously — any future reconciler / Phase 2
// KRT consumer can rely on the fields being split correctly.
func TestStore_NotifyPayloadDiscreteFields(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Dedicated listener on the agents status channel. Acquire it on a
	// separate connection so the INSERT inside store.Upsert doesn't race
	// the LISTEN (LISTEN must be established before the INSERT fires).
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "LISTEN v1alpha1_agents_status")
	require.NoError(t, err)

	// Name with `/` — the bomb. Pre-fix, payload was "default/ai.exa/exa/v1"
	// which splits four ways under strings.Split(id, "/") and consumers
	// can't tell whether the name was "ai.exa" (+ version "exa/v1") or
	// "ai.exa/exa" (+ version "v1"). Post-fix the fields are discrete.
	const nsName = "ai.exa/exa"
	_, err = store.Upsert(ctx, testNS, nsName, "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "slash"}), UpsertOpts{})
	require.NoError(t, err)

	// Drain one notification; guard against hangs in CI.
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	notif, err := conn.Conn().WaitForNotification(waitCtx)
	require.NoError(t, err, "expected a pg_notify from the INSERT")
	require.Equal(t, "v1alpha1_agents_status", notif.Channel)

	var payload struct {
		Op        string `json:"op"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Version   string `json:"version"`
	}
	require.NoError(t, json.Unmarshal([]byte(notif.Payload), &payload),
		"payload must be JSON with discrete (namespace, name, version) fields")
	require.Equal(t, "INSERT", payload.Op)
	require.Equal(t, testNS, payload.Namespace)
	require.Equal(t, nsName, payload.Name,
		"name must round-trip intact, including the / character")
	require.Equal(t, "v1", payload.Version)
}

// TestStore_LatestVersionTieBreakDeterministic guards the R6 fix:
// when two non-semver versions share an identical updated_at (possible
// on batch upserts inside a single microsecond), recomputeLatest must
// pick the same winner every time. Pre-fix the ORDER BY was
// `updated_at DESC` only and the winner was SQL-row-order dependent;
// post-fix the secondary `version DESC` key makes it deterministic.
//
// Force the tie by overwriting both rows' updated_at to an identical
// value + clearing is_latest_version, then run recomputeLatest directly
// (internal-package test can reach the unexported helper) so its SELECT
// actually sees the tied timestamps.
func TestStore_LatestVersionTieBreakDeterministic(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	// Two non-semver versions so pickLatestVersion falls through to
	// the "most recently updated" branch.
	_, err := store.Upsert(ctx, testNS, "tie", "snapshot-a", mustSpec(t, v1alpha1.AgentSpec{Title: "A"}), UpsertOpts{})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, testNS, "tie", "snapshot-b", mustSpec(t, v1alpha1.AgentSpec{Title: "B"}), UpsertOpts{})
	require.NoError(t, err)

	// Force identical updated_at + clear is_latest_version so the next
	// recomputeLatest has to choose from scratch under the tie.
	_, err = pool.Exec(ctx,
		`UPDATE `+testTable+` SET updated_at = $1, is_latest_version = false
		 WHERE namespace=$2 AND name=$3`,
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), testNS, "tie")
	require.NoError(t, err)

	// Run recomputeLatest directly inside a fresh tx so its SELECT sees
	// the tied timestamps. Repeat; deterministic tie-break must land on
	// the same winner every call.
	var winners []string
	for i := 0; i < 5; i++ {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		require.NoError(t, store.recomputeLatest(ctx, tx, testNS, "tie"))
		require.NoError(t, tx.Commit(ctx))

		latest, err := store.GetLatest(ctx, testNS, "tie")
		require.NoError(t, err)
		winners = append(winners, latest.Metadata.Version)
	}
	for i := 1; i < len(winners); i++ {
		require.Equal(t, winners[0], winners[i],
			"recomputeLatest must pick the same winner across repeated reads")
	}
	// `version DESC` tie-break → snapshot-b comes out on top.
	require.Equal(t, "snapshot-b", winners[0], "version DESC tie-break should prefer 'snapshot-b'")
}

// TestStore_UpsertAssignsUID pins the create-side of the metadata.uid
// contract: Postgres' column default fires on first insert, the RETURNING
// clause surfaces the assigned value to the caller, and a follow-up Get
// reads back the same UID.
func TestStore_UpsertAssignsUID(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	res, err := store.Upsert(ctx, testNS, "uid-create", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "first"}), UpsertOpts{})
	require.NoError(t, err)
	require.True(t, res.Created)
	require.Regexp(t, uuidPattern, res.UID, "UpsertResult.UID must be a canonical UUID")

	obj, err := store.Get(ctx, testNS, "uid-create", "v1")
	require.NoError(t, err)
	require.Equal(t, res.UID, obj.Metadata.UID, "Get must surface the same UID Upsert returned")
}

// TestStore_UpsertPreservesUIDAcrossUpdates pins the immutability half:
// the UID column lives outside the ON CONFLICT SET list, so neither a
// no-op re-apply (same spec) nor a spec-changing re-apply may rotate it.
// This is the Kubernetes-style read-only metadata.uid behavior — once
// observed by a controller, the UID stays stable for the row's lifetime.
func TestStore_UpsertPreservesUIDAcrossUpdates(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	first, err := store.Upsert(ctx, testNS, "uid-stable", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "first"}), UpsertOpts{})
	require.NoError(t, err)
	require.NotEmpty(t, first.UID)

	// No-op re-apply — generation stays at 1, UID must not change.
	noop, err := store.Upsert(ctx, testNS, "uid-stable", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "first"}), UpsertOpts{})
	require.NoError(t, err)
	require.False(t, noop.SpecChanged)
	require.Equal(t, first.UID, noop.UID, "no-op upsert must preserve UID")

	// Spec-changing re-apply — generation bumps, UID still must not change.
	bumped, err := store.Upsert(ctx, testNS, "uid-stable", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "second"}), UpsertOpts{})
	require.NoError(t, err)
	require.True(t, bumped.SpecChanged)
	require.EqualValues(t, 2, bumped.Generation)
	require.Equal(t, first.UID, bumped.UID, "spec-changing upsert must preserve UID")

	obj, err := store.Get(ctx, testNS, "uid-stable", "v1")
	require.NoError(t, err)
	require.Equal(t, first.UID, obj.Metadata.UID, "Get must keep returning the original UID")
}

// TestStore_UpsertGeneratesFreshUIDAfterRecreate is the whole reason UID
// exists: (namespace, name, version) is reusable across delete +
// recreate, so without UID, a controller that observed the original row
// can't tell when it has been replaced. The recreated row must carry a
// new UID so observers can detect the boundary.
func TestStore_UpsertGeneratesFreshUIDAfterRecreate(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	first, err := store.Upsert(ctx, testNS, "uid-recreate", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "v1-first"}), UpsertOpts{})
	require.NoError(t, err)
	require.NotEmpty(t, first.UID)

	// Finalizer-free hard-delete (matches the fast path Delete now takes
	// for kinds without finalizers — see TestStore_DeleteFinalizerFreeHardDeletes).
	require.NoError(t, store.Delete(ctx, testNS, "uid-recreate", "v1"))

	second, err := store.Upsert(ctx, testNS, "uid-recreate", "v1", mustSpec(t, v1alpha1.AgentSpec{Title: "v1-second"}), UpsertOpts{})
	require.NoError(t, err)
	require.True(t, second.Created, "post-delete upsert is a fresh create")
	require.NotEmpty(t, second.UID)
	require.NotEqual(t, first.UID, second.UID,
		"recreated row must carry a new UID so observers can distinguish it from the deleted predecessor")
}

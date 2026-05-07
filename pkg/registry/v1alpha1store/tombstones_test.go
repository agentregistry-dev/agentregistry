//go:build integration

package v1alpha1store

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// agentForTombstone is a per-file helper so the tombstone tests stay
// isolated from package-internal fixtures. modelName drives the spec
// hash so the same name can produce hash-equal or hash-distinct rows.
func agentForTombstone(namespace, name, modelName string) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: namespace, Name: name},
		Spec:     v1alpha1.AgentSpec{ModelName: modelName},
	}
}

// readTombstoneMax queries v1alpha1.version_tombstones directly so the
// tests can verify the high-water mark without exposing internals on
// Store. Returns 0 when no row exists.
func readTombstoneMax(t *testing.T, pool *pgxpool.Pool, table, namespace, name string) int {
	t.Helper()
	var maxAssigned int
	err := pool.QueryRow(context.Background(), `
		SELECT max_assigned
		FROM v1alpha1.version_tombstones
		WHERE table_name=$1 AND namespace=$2 AND name=$3`,
		table, namespace, name,
	).Scan(&maxAssigned)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0
	}
	require.NoError(t, err)
	return maxAssigned
}

// TestUpsert_AfterDeleteAll_DoesNotReuseVersion exercises the core
// promise of the tombstone table: once a (namespace, name) has been
// deleted, the next apply does NOT recycle v1.
func TestUpsert_AfterDeleteAll_DoesNotReuseVersion(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	res, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)

	require.NoError(t, store.DeleteAllVersions(ctx, "default", "foo"))

	res2, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-fresh"))
	require.NoError(t, err)
	require.Equal(t, 2, res2.Version, "tombstone must prevent v1 reuse after DeleteAllVersions")
	require.Equal(t, UpsertCreated, res2.Outcome)
}

// TestUpsert_AfterDeleteAll_ResumesFromHighWaterMark verifies that the
// tombstone tracks MAX(version), not just the count of inserts: three
// versions in, delete all, the next apply lands at v4.
func TestUpsert_AfterDeleteAll_ResumesFromHighWaterMark(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.Upsert(ctx, agentForTombstone("default", "foo", fmt.Sprintf("model-%d", i)))
		require.NoError(t, err)
	}
	require.Equal(t, 3, readTombstoneMax(t, pool, testTable, "default", "foo"))

	require.NoError(t, store.DeleteAllVersions(ctx, "default", "foo"))

	res, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-fresh"))
	require.NoError(t, err)
	require.Equal(t, 4, res.Version)
	require.Equal(t, 4, readTombstoneMax(t, pool, testTable, "default", "foo"))
}

// TestUpsert_TombstoneWritten_OnFirstInsert ensures the tombstone is
// populated even before any deletion has happened — Apply must
// always record the high-water mark so future deletes don't expose
// a gap.
func TestUpsert_TombstoneWritten_OnFirstInsert(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "default", "foo"))
}

// TestUpsert_TombstoneUnchanged_OnNoOp asserts that a hash-identical
// re-apply does not bump the tombstone — only successful INSERTs do.
func TestUpsert_TombstoneUnchanged_OnNoOp(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "default", "foo"))

	res, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, UpsertNoOp, res.Outcome)
	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "default", "foo"),
		"no-op apply must not bump the tombstone")
}

// TestUpsert_TombstoneAdvances_OnSpecChange asserts that branch 3 (spec
// hash differs → new version) advances the tombstone in lock-step with
// the row's version column.
func TestUpsert_TombstoneAdvances_OnSpecChange(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "default", "foo"))

	res, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-b"))
	require.NoError(t, err)
	require.Equal(t, 2, res.Version)
	require.Equal(t, UpsertCreated, res.Outcome)
	require.Equal(t, 2, readTombstoneMax(t, pool, testTable, "default", "foo"))
}

// TestUpsert_TombstonesAreNamespaceScoped verifies the tombstone PK
// includes namespace, so the same name in two namespaces gets two
// independent number lines.
func TestUpsert_TombstonesAreNamespaceScoped(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	r1, err := store.Upsert(ctx, agentForTombstone("default", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, r1.Version)

	r2, err := store.Upsert(ctx, agentForTombstone("other-ns", "foo", "model-a"))
	require.NoError(t, err)
	require.Equal(t, 1, r2.Version, "tombstone for (other-ns, foo) must be independent of (default, foo)")

	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "default", "foo"))
	require.Equal(t, 1, readTombstoneMax(t, pool, testTable, "other-ns", "foo"))
}

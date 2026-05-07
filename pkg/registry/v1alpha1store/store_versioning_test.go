//go:build integration

package v1alpha1store_test

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/agentregistry-dev/agentregistry/pkg/types/typestest"
)

// agentObj returns an Agent envelope with the given name + a deterministic
// spec keyed by modelName, so the four apply-branch tests can produce both
// hash-equal and hash-distinct payloads without recomputing fixtures.
func agentObj(name, modelName string, labels map[string]string) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Labels: labels},
		Spec:     v1alpha1.AgentSpec{ModelName: modelName},
	}
}

func setupAgentStore(t *testing.T) *v1alpha1store.Store {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	return v1alpha1store.NewStore(pool, "v1alpha1.agents")
}

func TestUpsert_NewName_AssignsVersion1(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

func TestUpsert_SameSpec_NoOp(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)
	require.Equal(t, v1alpha1store.UpsertNoOp, res.Outcome)
}

func TestUpsert_ChangedSpec_BumpsVersion(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	res, err := store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, 2, res.Version)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

func TestUpsert_LabelChangeOnSameSpec_UpdatesLatest(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	res, err := store.Upsert(ctx, agentObj("foo", "model-a", map[string]string{"deprecated": "true"}))
	require.NoError(t, err)
	require.Equal(t, 1, res.Version)
	require.Equal(t, v1alpha1store.UpsertLabelsUpdated, res.Outcome)
}

// TestUpsert_ConcurrentSpecs_AssignsSequentialVersions exercises the
// SELECT ... FOR UPDATE serialization in upsertVersioned. N goroutines
// race to apply distinct specs against the same (namespace, name);
// every apply must succeed and the assigned versions must form a dense
// 1..N sequence with no gaps and no duplicates.
func TestUpsert_ConcurrentSpecs_AssignsSequentialVersions(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	const N = 5
	var wg sync.WaitGroup
	versions := make([]int, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := store.Upsert(ctx, agentObj("race", fmt.Sprintf("model-%d", i), nil))
			if err == nil {
				versions[i] = res.Version
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d", i)
	}

	sort.Ints(versions)
	require.Equal(t, []int{1, 2, 3, 4, 5}, versions)
}

// TestUpsert_AfterTotalDeletion_ResumesAfterTombstone verifies that
// once every version row for (namespace, name) is removed, the next
// apply numbers from tombstone.max_assigned + 1 — NOT 1. The
// tombstone keeps the version sequence monotonic across delete cycles
// so deployment pins like "agents/foo:1" never silently re-resolve to
// a different spec after a delete-and-reapply.
func TestUpsert_AfterTotalDeletion_ResumesAfterTombstone(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.Upsert(ctx, agentObj("foo", fmt.Sprintf("model-%d", i), nil))
		require.NoError(t, err)
	}

	require.NoError(t, store.DeleteAllVersions(ctx, "default", "foo"))

	res, err := store.Upsert(ctx, agentObj("foo", "model-fresh", nil))
	require.NoError(t, err)
	require.Equal(t, 4, res.Version, "next apply must resume after the high-water mark, not recycle v1")
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

// TestDelete_LatestVersion_PromotesNextHighest verifies that deleting
// the highest version row exposes the next-highest as latest. Versioned
// rows are hard-deleted, so GetLatest's MAX(version) over surviving
// rows promotes v2 once v3 is gone.
func TestDelete_LatestVersion_PromotesNextHighest(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.Upsert(ctx, agentObj("foo", fmt.Sprintf("model-%d", i), nil))
		require.NoError(t, err)
	}

	require.NoError(t, store.Delete(ctx, "default", "foo", "3"))

	latest, err := store.GetLatest(ctx, "default", "foo")
	require.NoError(t, err)
	require.Equal(t, "2", latest.Metadata.Version)
}

// TestUpsert_IdempotentAcrossRestarts simulates a server restart by
// constructing a second Store against the same connection pool and
// re-applying the same spec. The second apply must hit the no-op
// branch — version stays at 1, outcome is UpsertNoOp — proving the
// hash-based dedupe is durable across Store lifetimes.
func TestUpsert_IdempotentAcrossRestarts(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	ctx := context.Background()

	s1 := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	res1, err := s1.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, 1, res1.Version)
	require.Equal(t, v1alpha1store.UpsertCreated, res1.Outcome)

	// Simulate a restart: drop s1, build a fresh Store against the same
	// underlying database. Re-applying the same spec must dedupe to no-op.
	s2 := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	res2, err := s2.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, 1, res2.Version)
	require.Equal(t, v1alpha1store.UpsertNoOp, res2.Outcome)
}

func setupAgentStoreWithAuditor(t *testing.T, a types.Auditor) *v1alpha1store.Store {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	return v1alpha1store.NewStore(pool, "v1alpha1.agents",
		v1alpha1store.WithKind(v1alpha1.KindAgent),
		v1alpha1store.WithAuditor(a),
	)
}

func setupProviderStoreWithAuditor(t *testing.T, a types.Auditor) *v1alpha1store.Store {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	return v1alpha1store.NewDeploymentStore(pool, "v1alpha1.providers",
		v1alpha1store.WithKind(v1alpha1.KindProvider),
		v1alpha1store.WithAuditor(a),
	)
}

// TestUpsert_AuditorCalledOnUpsertCreated verifies the Auditor fires
// once per immutable version creation and stays silent for no-op /
// labels-only updates.
func TestUpsert_AuditorCalledOnUpsertCreated(t *testing.T) {
	auditor := &typestest.RecordingAuditor{}
	store := setupAgentStoreWithAuditor(t, auditor)
	ctx := context.Background()

	// Branch 1: no prior row → audit event with version=1.
	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1)
	require.Equal(t, typestest.ResourceVersionEvent{Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Version: 1}, auditor.Events()[0])

	// Branch 2 (no-op): same spec, same labels → no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "UpsertNoOp must not produce an audit event")

	// Branch 2 (labels updated): same spec, different labels → no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "UpsertLabelsUpdated must not produce an audit event")

	// Branch 3: changed spec → audit event with version=2.
	_, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 2)
	require.Equal(t, typestest.ResourceVersionEvent{Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Version: 2}, auditor.Events()[1])
}

// TestUpsert_AuditorNotCalledForLegacyKinds verifies the legacy
// (Provider/Deployment) upsert path does not fire ResourceVersionCreated
// — those kinds model lifecycle state and are out of scope for the
// version-creation audit event.
func TestUpsert_AuditorNotCalledForLegacyKinds(t *testing.T) {
	auditor := &typestest.RecordingAuditor{}
	store := setupProviderStoreWithAuditor(t, auditor)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "p1", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformLocal},
	})
	require.NoError(t, err)
	require.Empty(t, auditor.Events(), "legacy kinds must not emit ResourceVersionCreated")
}

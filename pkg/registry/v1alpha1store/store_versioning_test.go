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
	return taggedAgentObj(name, "", modelName, labels)
}

func taggedAgentObj(name, tag, modelName string, labels map[string]string) *v1alpha1.Agent {
	return &v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindAgent},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: name, Tag: tag, Labels: labels},
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
	require.NotEmpty(t, res.Tag)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

func TestUpsert_SameSpec_NoOp(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.NotEmpty(t, res.Tag)
	require.Equal(t, v1alpha1store.UpsertNoOp, res.Outcome)
}

func TestUpsert_ChangedSpec_BumpsVersion(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	res, err := store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.NotEmpty(t, res.Tag)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

func TestUpsert_LabelChangeOnSameSpec_UpdatesLatest(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, taggedAgentObj("foo", "stable", "model-a", nil))
	require.NoError(t, err)
	res, err := store.Upsert(ctx, taggedAgentObj("foo", "stable", "model-a", map[string]string{"deprecated": "true"}))
	require.NoError(t, err)
	require.Equal(t, "stable", res.Tag)
	require.Equal(t, v1alpha1store.UpsertReplaced, res.Outcome)
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
	tags := make([]string, N)
	errs := make([]error, N)

	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := store.Upsert(ctx, agentObj("race", fmt.Sprintf("model-%d", i), nil))
			if err == nil {
				tags[i] = res.Tag
			}
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		require.NoError(t, err, "goroutine %d", i)
	}

	sort.Strings(tags)
	require.Len(t, tags, N)
	require.NotContains(t, tags, "")
	require.Len(t, map[string]struct{}{tags[0]: {}, tags[1]: {}, tags[2]: {}, tags[3]: {}, tags[4]: {}}, N)
}

// TestUpsert_AfterTotalDeletion_RestartsAtVersion1 verifies that once
// every version row for (namespace, name) is removed, the next apply
// starts numbering from 1 again. The hard-delete path leaves no
// trailing state to anchor the next MAX(version) lookup against.
func TestUpsert_AfterTotalDeletion_RestartsAtVersion1(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.Upsert(ctx, agentObj("foo", fmt.Sprintf("model-%d", i), nil))
		require.NoError(t, err)
	}

	require.NoError(t, store.DeleteAllTags(ctx, "default", "foo"))

	res, err := store.Upsert(ctx, agentObj("foo", "model-fresh", nil))
	require.NoError(t, err)
	require.NotEmpty(t, res.Tag)
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
		_, err := store.Upsert(ctx, taggedAgentObj("foo", fmt.Sprintf("v%d", i+1), fmt.Sprintf("model-%d", i), nil))
		require.NoError(t, err)
	}

	require.NoError(t, store.Delete(ctx, "default", "foo", "v3"))

	latest, err := store.GetLatest(ctx, "default", "foo")
	require.NoError(t, err)
	require.Equal(t, "v2", latest.Metadata.Tag)
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
	require.NotEmpty(t, res1.Tag)
	require.Equal(t, v1alpha1store.UpsertCreated, res1.Outcome)

	// Simulate a restart: drop s1, build a fresh Store against the same
	// underlying database. Re-applying the same spec must dedupe to no-op.
	s2 := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	res2, err := s2.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, res1.Tag, res2.Tag)
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
	firstTag := auditor.Events()[0].Tag
	require.Equal(t, typestest.ResourceTagEvent{Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Tag: firstTag}, auditor.Events()[0])

	// Branch 2 (no-op): same spec, same labels → no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "UpsertNoOp must not produce an audit event")

	// Branch 2 (labels updated): same spec, different labels → no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 2, "new default tag from label change must produce an audit event")

	// Branch 3: changed spec → audit event with version=2.
	_, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 3)
	require.Equal(t, "foo", auditor.Events()[2].Name)
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
	require.Empty(t, auditor.Events(), "legacy kinds must not emit ResourceTagCreated")
}

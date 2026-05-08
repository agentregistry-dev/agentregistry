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

func TestUpsert_NewName_DefaultsTagLatest(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

func TestUpsert_SameSpec_NoOp(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)
	require.Equal(t, v1alpha1store.UpsertNoOp, res.Outcome)
}

func TestUpsert_ChangedSpec_ReplacesDefaultTag(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	res, err := store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)
	require.Equal(t, v1alpha1store.UpsertReplaced, res.Outcome)
}

func TestUpsert_SameTagGenerationTracksDeclarativeChanges(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	res, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.EqualValues(t, 1, res.Generation)

	res, err = store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertNoOp, res.Outcome)
	require.EqualValues(t, 1, res.Generation)

	res, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertReplaced, res.Outcome)
	require.EqualValues(t, 2, res.Generation)

	row, err := store.Get(ctx, "default", "foo", v1alpha1store.DefaultTag())
	require.NoError(t, err)
	require.EqualValues(t, 2, row.Metadata.Generation)
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

// TestUpsert_ConcurrentExplicitTags exercises the SELECT ... FOR UPDATE
// serialization in upsertTagged. N goroutines race to apply distinct explicit
// tags against the same (namespace, name); every apply must succeed with its
// requested tag.
func TestUpsert_ConcurrentExplicitTags(t *testing.T) {
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
			tag := fmt.Sprintf("tag-%d", i)
			res, err := store.Upsert(ctx, taggedAgentObj("race", tag, fmt.Sprintf("model-%d", i), nil))
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

// TestUpsert_AfterTotalDeletion_RecreatesLatest verifies that once every tag
// row for (namespace, name) is removed, the next blank-tag apply recreates the
// literal latest tag.
func TestUpsert_AfterTotalDeletion_RecreatesLatest(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		_, err := store.Upsert(ctx, agentObj("foo", fmt.Sprintf("model-%d", i), nil))
		require.NoError(t, err)
	}

	require.NoError(t, store.DeleteAllTags(ctx, "default", "foo"))

	res, err := store.Upsert(ctx, agentObj("foo", "model-fresh", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), res.Tag)
	require.Equal(t, v1alpha1store.UpsertCreated, res.Outcome)
}

// TestDelete_LatestTagDoesNotPromoteAnotherTag verifies that "latest" is a
// literal tag, not a derived pointer to the most recently applied row.
func TestDelete_LatestTagDoesNotPromoteAnotherTag(t *testing.T) {
	store := setupAgentStore(t)
	ctx := context.Background()

	_, err := store.Upsert(ctx, taggedAgentObj("foo", "stable", "model-stable", nil))
	require.NoError(t, err)
	_, err = store.Upsert(ctx, agentObj("foo", "model-latest", nil))
	require.NoError(t, err)

	require.NoError(t, store.Delete(ctx, "default", "foo", v1alpha1store.DefaultTag()))

	_, err = store.GetLatest(ctx, "default", "foo")
	require.Error(t, err)
}

// TestUpsert_IdempotentAcrossRestarts simulates a server restart by
// constructing a second Store against the same connection pool and
// re-applying the same spec. The second apply must hit the no-op
// branch — tag stays latest, outcome is UpsertNoOp — proving same-tag dedupe
// is durable across Store lifetimes.
func TestUpsert_IdempotentAcrossRestarts(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	ctx := context.Background()

	s1 := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	res1, err := s1.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.DefaultTag(), res1.Tag)
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
	return v1alpha1store.NewMutableObjectStore(pool, "v1alpha1.providers",
		v1alpha1store.WithKind(v1alpha1.KindProvider),
		v1alpha1store.WithAuditor(a),
	)
}

// TestUpsert_AuditorCalledOnUpsertCreated verifies the Auditor fires when a
// new tag row is created and stays silent for no-op or replacement writes.
func TestUpsert_AuditorCalledOnUpsertCreated(t *testing.T) {
	auditor := &typestest.RecordingAuditor{}
	store := setupAgentStoreWithAuditor(t, auditor)
	ctx := context.Background()

	// Branch 1: no prior row → audit event with tag latest.
	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1)
	firstTag := auditor.Events()[0].Tag
	require.Equal(t, v1alpha1store.DefaultTag(), firstTag)
	require.Equal(t, typestest.ResourceTagEvent{Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Tag: firstTag}, auditor.Events()[0])

	// Branch 2 (no-op): same spec, same labels → no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "UpsertNoOp must not produce an audit event")

	// Branch 3 (labels updated): same tag, different labels → replacement,
	// but no new tag-row audit event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "same-tag replacement must not produce an audit event")

	// Branch 3: changed spec under same tag → replacement, still no event.
	_, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1)

	_, err = store.Upsert(ctx, taggedAgentObj("foo", "stable", "model-b", nil))
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 2)
	require.Equal(t, "stable", auditor.Events()[1].Tag)
}

// TestUpsert_AuditorNotCalledForMutableObjectKinds verifies the
// Provider/Deployment upsert path does not fire ResourceTagCreated; those kinds
// model lifecycle state and are out of scope for tag-creation audit events.
func TestUpsert_AuditorNotCalledForMutableObjectKinds(t *testing.T) {
	auditor := &typestest.RecordingAuditor{}
	store := setupProviderStoreWithAuditor(t, auditor)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "p1"},
		Spec:     v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformLocal},
	})
	require.NoError(t, err)
	require.Empty(t, auditor.Events(), "mutable-object kinds must not emit ResourceTagCreated")
}

//go:build integration

package v1alpha1store_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
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

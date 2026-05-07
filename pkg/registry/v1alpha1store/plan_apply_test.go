//go:build integration

package v1alpha1store_test

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/agentregistry-dev/agentregistry/pkg/types/typestest"
)

// rowCount returns the number of rows in v1alpha1.agents matching
// (namespace, name). Used by the "Plan does not write" assertion.
func rowCount(t *testing.T, pool *pgxpool.Pool, namespace, name string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM v1alpha1.agents WHERE namespace=$1 AND name=$2`,
		namespace, name,
	).Scan(&n)
	require.NoError(t, err)
	return n
}

// TestPlan_NewName_ProposesV1Created verifies the no-prior-row branch:
// Plan must report Created at version 1 with no row written.
func TestPlan_NewName_ProposesV1Created(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	plan, err := store.Plan(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertCreated, plan.Outcome)
	require.Equal(t, 1, plan.Version)
	require.Equal(t, "default", plan.Namespace)
	require.Equal(t, "foo", plan.Name)

	require.Equal(t, 0, rowCount(t, pool, "default", "foo"), "Plan must not write rows")
}

// TestPlan_SameSpec_ProposesNoOp covers branch 2 (hash equality, same
// labels): Plan reports NoOp at the existing version with no row
// changes.
func TestPlan_SameSpec_ProposesNoOp(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	plan, err := store.Plan(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertNoOp, plan.Outcome)
	require.Equal(t, 1, plan.Version)
}

// TestPlan_LabelChange_ProposesLabelsUpdated covers branch 2 with
// label drift: Plan reports LabelsUpdated at the existing version.
func TestPlan_LabelChange_ProposesLabelsUpdated(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	plan, err := store.Plan(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertLabelsUpdated, plan.Outcome)
	require.Equal(t, 1, plan.Version)
}

// TestPlan_SpecChange_ProposesNextVersion covers branch 3: Plan
// reports Created at MAX(version)+1.
func TestPlan_SpecChange_ProposesNextVersion(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	plan, err := store.Plan(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertCreated, plan.Outcome)
	require.Equal(t, 2, plan.Version)

	require.Equal(t, 1, rowCount(t, pool, "default", "foo"), "Plan must not write rows")
}

// TestPlanApply_RoundTripsLikeUpsert asserts every (Plan, Apply) pair
// produces the same UpsertResult Upsert would have. Sanity check on
// the planning seam refactor.
func TestPlanApply_RoundTripsLikeUpsert(t *testing.T) {
	cases := []struct {
		name    string
		first   *v1alpha1.Agent
		second  *v1alpha1.Agent
		outcome v1alpha1store.UpsertOutcome
		version int
	}{
		{"created-v1", nil, agentObj("a1", "model-a", nil), v1alpha1store.UpsertCreated, 1},
		{"noop", agentObj("a2", "model-a", nil), agentObj("a2", "model-a", nil), v1alpha1store.UpsertNoOp, 1},
		{"labels", agentObj("a3", "model-a", nil), agentObj("a3", "model-a", map[string]string{"env": "prod"}), v1alpha1store.UpsertLabelsUpdated, 1},
		{"spec-change", agentObj("a4", "model-a", nil), agentObj("a4", "model-b", nil), v1alpha1store.UpsertCreated, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pool := v1alpha1store.NewTestPool(t)
			store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
			ctx := context.Background()

			if tc.first != nil {
				_, err := store.Upsert(ctx, tc.first)
				require.NoError(t, err)
			}

			plan, err := store.Plan(ctx, tc.second)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, plan.Outcome)
			require.Equal(t, tc.version, plan.Version)

			res, err := store.Apply(ctx, plan)
			require.NoError(t, err)
			require.Equal(t, tc.outcome, res.Outcome)
			require.Equal(t, tc.version, res.Version)
		})
	}
}

// TestApply_StaleAfterConcurrentVersionBump covers the canonical
// approval-race: A plans at v1 (existing row), B applies a new spec
// (v2) before A applies. A's Apply must reject as ErrPlanStale rather
// than silently overwriting B's intent.
func TestApply_StaleAfterConcurrentVersionBump(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	// Approver A plans a label-only change; Plan witnesses v1 + hash-A.
	planA, err := store.Plan(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertLabelsUpdated, planA.Outcome)

	// Concurrent writer B applies a spec change → row at v2. The
	// witness Plan A captured (latest=v1, hash=A) is now stale.
	_, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)

	_, err = store.Apply(ctx, planA)
	require.ErrorIs(t, err, v1alpha1store.ErrPlanStale)
}

// TestApply_StaleAfterRowDeleted covers the case where Plan witnessed
// a live row but DeleteAllVersions removed it before Apply ran. The
// witness's `found` flag no longer matches reality.
func TestApply_StaleAfterRowDeleted(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	_, err := store.Upsert(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)

	plan, err := store.Plan(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertCreated, plan.Outcome)
	require.Equal(t, 2, plan.Version)

	require.NoError(t, store.DeleteAllVersions(ctx, "default", "foo"))

	_, err = store.Apply(ctx, plan)
	require.ErrorIs(t, err, v1alpha1store.ErrPlanStale)
}

// TestApply_StaleAfterTombstoneShift covers the subtle case where
// Plan saw "no live row" but a separate writer applied + deleted
// between Plan and Apply, advancing the tombstone. The witness's
// tombstone snapshot disagrees with the live one, so Apply must
// reject — using plan.Version would otherwise insert at a recycled
// number.
func TestApply_StaleAfterTombstoneShift(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewStore(pool, "v1alpha1.agents")
	ctx := context.Background()

	planA, err := store.Plan(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertCreated, planA.Outcome)
	require.Equal(t, 1, planA.Version)

	// Concurrent writer applies and deletes — tombstone now says
	// max_assigned=1, even though no live row remains.
	_, err = store.Upsert(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.NoError(t, store.DeleteAllVersions(ctx, "default", "foo"))

	_, err = store.Apply(ctx, planA)
	require.ErrorIs(t, err, v1alpha1store.ErrPlanStale)
}

func setupAgentStoreForAuditTest(t *testing.T, a types.Auditor) (*v1alpha1store.Store, *pgxpool.Pool) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	return v1alpha1store.NewStore(pool, "v1alpha1.agents",
		v1alpha1store.WithKind(v1alpha1.KindAgent),
		v1alpha1store.WithAuditor(a),
	), pool
}

// TestApply_FiresAuditorOnlyOnCreated asserts the audit gate moved
// cleanly from Upsert into Apply: Plan never fires, NoOp / LabelsUpdated
// never fire, only Apply on UpsertCreated does.
func TestApply_FiresAuditorOnlyOnCreated(t *testing.T) {
	auditor := &typestest.RecordingAuditor{}
	store, _ := setupAgentStoreForAuditTest(t, auditor)
	ctx := context.Background()

	// Plan-only must NOT fire audit.
	plan, err := store.Plan(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Empty(t, auditor.Events(), "Plan must never fire audit")

	// Apply (Created) → 1 event.
	_, err = store.Apply(ctx, plan)
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1)
	require.Equal(t, typestest.ResourceVersionEvent{
		Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Version: 1,
	}, auditor.Events()[0])

	// Plan + Apply NoOp → no new event.
	planNoop, err := store.Plan(ctx, agentObj("foo", "model-a", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertNoOp, planNoop.Outcome)
	_, err = store.Apply(ctx, planNoop)
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "Apply on NoOp must not fire audit")

	// Plan + Apply LabelsUpdated → no new event.
	planLabels, err := store.Plan(ctx, agentObj("foo", "model-a", map[string]string{"env": "prod"}))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertLabelsUpdated, planLabels.Outcome)
	_, err = store.Apply(ctx, planLabels)
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 1, "Apply on LabelsUpdated must not fire audit")

	// Plan + Apply Created (spec change) → 1 more event.
	planV2, err := store.Plan(ctx, agentObj("foo", "model-b", nil))
	require.NoError(t, err)
	require.Equal(t, v1alpha1store.UpsertCreated, planV2.Outcome)
	_, err = store.Apply(ctx, planV2)
	require.NoError(t, err)
	require.Len(t, auditor.Events(), 2)
	require.Equal(t, typestest.ResourceVersionEvent{
		Kind: v1alpha1.KindAgent, Namespace: "default", Name: "foo", Version: 2,
	}, auditor.Events()[1])
}

// TestPlan_LegacyStore_Rejects pins the documented error for the
// legacy (Provider/Deployment) path. Plan/Apply only support
// versioned-artifact stores; legacy callers stay on Upsert.
func TestPlan_LegacyStore_Rejects(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewDeploymentStore(pool, "v1alpha1.providers",
		v1alpha1store.WithKind(v1alpha1.KindProvider),
	)
	ctx := context.Background()

	_, err := store.Plan(ctx, &v1alpha1.Provider{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindProvider},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "p1", Version: "1"},
		Spec:     v1alpha1.ProviderSpec{Platform: v1alpha1.PlatformLocal},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "Plan/Apply only supported for versioned-artifact stores")
}

// TestApply_LegacyStore_Rejects mirrors TestPlan_LegacyStore_Rejects
// for the Apply entrypoint — same gate, same error.
func TestApply_LegacyStore_Rejects(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	store := v1alpha1store.NewDeploymentStore(pool, "v1alpha1.providers")
	ctx := context.Background()

	_, err := store.Apply(ctx, v1alpha1store.UpsertPlan{
		Namespace: "default",
		Name:      "p1",
		Version:   1,
		Outcome:   v1alpha1store.UpsertCreated,
	})
	require.Error(t, err)
	// errPlanLegacy is unexported; match by message text.
	require.Contains(t, err.Error(), "Plan/Apply only supported for versioned-artifact stores")
}

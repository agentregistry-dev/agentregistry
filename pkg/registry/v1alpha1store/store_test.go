//go:build integration

package v1alpha1store

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

const testTable = "v1alpha1.agents"
const testNS = "default"

// upsertAgent is a small helper that builds an Agent envelope from
// (name, spec, labels) and applies it without metadata.tag. The store
// defaults blank tags to the literal "latest" tag.
func upsertAgent(t *testing.T, store *Store, name string, spec v1alpha1.AgentSpec, labels map[string]string) UpsertResult {
	t.Helper()
	res, err := store.Upsert(context.Background(), &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: name, Labels: labels},
		Spec:     spec,
	})
	require.NoError(t, err)
	return res
}

func TestStore_UpsertCreatesRow(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	res, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "foo"},
		Spec:     v1alpha1.AgentSpec{Title: "alpha"},
	})
	require.NoError(t, err)
	require.Equal(t, UpsertCreated, res.Outcome)
	require.Equal(t, DefaultTag(), res.Tag)

	obj, err := store.Get(ctx, testNS, "foo", DefaultTag())
	require.NoError(t, err)
	require.Equal(t, testNS, obj.Metadata.Namespace)
	require.Equal(t, "foo", obj.Metadata.Name)
	require.Equal(t, DefaultTag(), obj.Metadata.Tag)
	require.False(t, obj.Metadata.CreatedAt.IsZero())
}

// TestStore_UpsertNoOpOnIdenticalSpec verifies the new apply-branch
// semantics: same spec_hash + same labels/annotations is a no-op.
func TestStore_UpsertNoOpOnIdenticalSpec(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)

	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "alpha"}, nil)
	res := upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "alpha"}, nil)
	require.Equal(t, UpsertNoOp, res.Outcome)
	require.Equal(t, DefaultTag(), res.Tag)
}

// TestStore_UpsertReplacesLatestOnSpecChange verifies that a changed payload
// for the same default tag atomically replaces the previous latest row.
func TestStore_UpsertReplacesLatestOnSpecChange(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)

	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "first"}, nil)
	res := upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "second"}, nil)
	require.Equal(t, UpsertReplaced, res.Outcome)
	require.Equal(t, DefaultTag(), res.Tag)

	obj, err := store.Get(context.Background(), testNS, "foo", DefaultTag())
	require.NoError(t, err)
	require.Equal(t, DefaultTag(), obj.Metadata.Tag)
}

// TestStore_GetLatestReadsLiteralLatestTag verifies that GetLatest returns the
// row tagged "latest", not the newest or lexicographically highest tag.
func TestStore_GetLatestReadsLiteralLatestTag(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)

	for _, tag := range []string{"stable", "candidate"} {
		_, err := store.Upsert(context.Background(), &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "foo", Tag: tag},
			Spec:     v1alpha1.AgentSpec{Title: tag},
		})
		require.NoError(t, err)
	}
	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "current"}, nil)

	latest, err := store.GetLatest(context.Background(), testNS, "foo")
	require.NoError(t, err)
	require.Equal(t, DefaultTag(), latest.Metadata.Tag)
}

func TestStore_PatchStatusDisjointFromSpec(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "alpha"}, nil)

	// Store.PatchStatus takes an opaque-bytes mutator; the typed
	// Status callback wraps through v1alpha1.StatusPatcher.
	err := store.PatchStatus(ctx, testNS, "foo", DefaultTag(), v1alpha1.StatusPatcher(func(s *v1alpha1.Status) {
		s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue, Reason: "Converged"})
	}))
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "foo", DefaultTag())
	require.NoError(t, err)
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(obj.Status, &status))
	require.Len(t, status.Conditions, 1)
	require.Equal(t, "Ready", status.Conditions[0].Type)
	require.Equal(t, v1alpha1.ConditionTrue, status.Conditions[0].Status)
}

func TestStore_PatchStatusNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	err := store.PatchStatus(ctx, testNS, "nope", "1", v1alpha1.StatusPatcher(func(*v1alpha1.Status) {}))
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_GetNotFound(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Get(ctx, testNS, "nope", "1")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))

	_, err = store.GetLatest(ctx, testNS, "nope")
	require.True(t, errors.Is(err, pkgdb.ErrNotFound))
}

// TestStore_DeleteHardDeletesTaggedRow guards the tagged-artifact fast path:
// rows have no finalizers and Delete
// hard-deletes immediately. arctl delete + arctl apply works without any
// background GC pass.
func TestStore_DeleteHardDeletesTaggedRow(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "foo", Tag: "stable"},
		Spec:     v1alpha1.AgentSpec{Title: "stable"},
	})
	require.NoError(t, err)
	upsertAgent(t, store, "foo", v1alpha1.AgentSpec{Title: "current"}, nil)

	require.NoError(t, store.Delete(ctx, testNS, "foo", DefaultTag()))

	// GetLatest is literal tag lookup, so deleting "latest" does not promote
	// another tag.
	_, err = store.GetLatest(ctx, testNS, "foo")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)

	// latest is gone — fully removed, while the explicit tag remains.
	_, err = store.Get(ctx, testNS, "foo", DefaultTag())
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
	stable, err := store.Get(ctx, testNS, "foo", "stable")
	require.NoError(t, err)
	require.Equal(t, "stable", stable.Metadata.Tag)

	// Re-apply with the same logical identity succeeds as a fresh latest tag.
	res := upsertAgent(t, store, "bar", v1alpha1.AgentSpec{Title: "reborn"}, nil)
	require.Equal(t, UpsertCreated, res.Outcome)
	require.Equal(t, DefaultTag(), res.Tag)

	// Deleting a missing tag still errors.
	err = store.Delete(ctx, testNS, "foo", "99")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)
}

func TestStore_List(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "a", Labels: map[string]string{"owner": "x"}},
		Spec:     v1alpha1.AgentSpec{Title: "A"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: "b", Labels: map[string]string{"owner": "y"}},
		Spec:     v1alpha1.AgentSpec{Title: "B"},
	})
	require.NoError(t, err)
	_, err = store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: "team-b", Name: "c", Labels: map[string]string{"owner": "x"}},
		Spec:     v1alpha1.AgentSpec{Title: "C"},
	})
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

	require.NoError(t, store.Delete(ctx, "team-a", "a", DefaultTag()))

	alive, _, err := store.List(ctx, ListOpts{})
	require.NoError(t, err)
	require.Len(t, alive, 2)
}

func TestStore_ListExtraWhereRebasesPlaceholders(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"a", "b", "c"} {
		_, err := store.Upsert(ctx, &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Namespace: "team-a", Name: name},
			Spec:     v1alpha1.AgentSpec{Title: name},
		})
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
// wrong query.
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
		{"args supplied but fragment has no placeholder", "deletion_timestamp IS NULL", []any{"a"}},
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
		ExtraWhere: "name = $1 OR name = $1",
		ExtraArgs:  []any{"x"},
	})
	require.NoError(t, err)
}

func TestStore_ListCursorPagination(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	for _, name := range []string{"first", "second", "third"} {
		upsertAgent(t, store, name, v1alpha1.AgentSpec{Title: name}, nil)
	}

	page1, nextCursor, err := store.List(ctx, ListOpts{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.NotEmpty(t, nextCursor)

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
// reason List orders by (namespace, name, tag/version, updated_at) ASC
// rather than updated_at DESC: a row whose updated_at moves under a
// concurrent PatchStatus must not jump pages or get returned twice.
func TestStore_ListCursorStableUnderStatusChurn(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	names := []string{"alpha", "beta", "gamma", "delta"} // lexical order: alpha, beta, delta, gamma
	for _, n := range names {
		upsertAgent(t, store, n, v1alpha1.AgentSpec{Title: n}, nil)
	}

	page1, cursor, err := store.List(ctx, ListOpts{Limit: 2})
	require.NoError(t, err)
	require.Len(t, page1, 2)
	require.Equal(t, "alpha", page1[0].Metadata.Name)
	require.Equal(t, "beta", page1[1].Metadata.Name)

	require.NoError(t, store.PatchStatus(ctx, testNS, "alpha", DefaultTag(), func(json.RawMessage) (json.RawMessage, error) {
		return json.RawMessage(`{"version":1}`), nil
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

	_, err := store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "annotated", Annotations: map[string]string{"keep": "me"}},
		Spec:     v1alpha1.AgentSpec{Title: "annotated"},
	})
	require.NoError(t, err)

	err = store.PatchAnnotations(ctx, testNS, "annotated", DefaultTag(), func(annotations map[string]string) map[string]string {
		annotations["add"] = "value"
		return annotations
	})
	require.NoError(t, err)

	obj, err := store.Get(ctx, testNS, "annotated", DefaultTag())
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

	_, err := agents.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "refs-bar"},
		Spec: v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "bar", Version: "1"}},
		},
	})
	require.NoError(t, err)

	_, err = agents.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: "refs-baz"},
		Spec: v1alpha1.AgentSpec{
			MCPServers: []v1alpha1.ResourceRef{{Kind: v1alpha1.KindMCPServer, Name: "baz", Version: "1"}},
		},
	})
	require.NoError(t, err)

	pattern, err := json.Marshal(map[string]any{
		"mcpServers": []map[string]string{{"name": "bar", "version": "1"}},
	})
	require.NoError(t, err)

	results, err := agents.FindReferrers(ctx, pattern, FindReferrersOpts{})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Equal(t, "refs-bar", results[0].Metadata.Name)
}

func TestStore_SeededProviders(t *testing.T) {
	pool := NewTestPool(t)
	// Provider is legacy-mode (string version, is_latest_version flag);
	// use the NewDeploymentStore constructor.
	providers := NewDeploymentStore(pool, "v1alpha1.providers")
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
// the status NOTIFY trigger emits (namespace, name, tag) as three
// discrete JSON fields instead of a concatenated "ns/name/tag" string.
func TestStore_NotifyPayloadDiscreteFields(t *testing.T) {
	pool := NewTestPool(t)
	store := NewStore(pool, testTable)
	ctx := context.Background()

	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	defer conn.Release()
	_, err = conn.Exec(ctx, "LISTEN v1alpha1_agents_status")
	require.NoError(t, err)

	// Name with `/` keeps the discrete-fields wire format intact.
	const nsName = "ai.exa/exa"
	_, err = store.Upsert(ctx, &v1alpha1.Agent{
		Metadata: v1alpha1.ObjectMeta{Namespace: testNS, Name: nsName},
		Spec:     v1alpha1.AgentSpec{Title: "slash"},
	})
	require.NoError(t, err)

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	notif, err := conn.Conn().WaitForNotification(waitCtx)
	require.NoError(t, err, "expected a pg_notify from the INSERT")
	require.Equal(t, "v1alpha1_agents_status", notif.Channel)

	var payload struct {
		Op        string `json:"op"`
		Namespace string `json:"namespace"`
		Name      string `json:"name"`
		Tag       string `json:"version"`
	}
	require.NoError(t, json.Unmarshal([]byte(notif.Payload), &payload),
		"payload must be JSON with discrete (namespace, name, tag) fields")
	require.Equal(t, "INSERT", payload.Op)
	require.Equal(t, testNS, payload.Namespace)
	require.Equal(t, nsName, payload.Name,
		"name must round-trip intact, including the / character")
	require.Equal(t, DefaultTag(), payload.Tag, "tag emitted as the default latest tag")
}

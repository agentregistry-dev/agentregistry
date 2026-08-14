//go:build integration

package v1alpha1store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestRemoveLocalRuntimeSeedMigrationUpgradeAndRollback(t *testing.T) {
	pool, dsn := NewTestPoolWithDSN(t, adminDSN())
	ctx := context.Background()

	migrator, err := NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(12))

	_, err = pool.Exec(ctx, `
		INSERT INTO runtimes (namespace, name, spec)
		VALUES
			('default', 'custom-local', '{"type":"Local"}'::jsonb),
			('default', 'custom-kubernetes', '{"type":"Kubernetes"}'::jsonb)
	`)
	require.NoError(t, err)

	require.NoError(t, migrator.Up())

	runtimes := NewMutableObjectStore(pool, TestSchema(), "runtimes")
	_, err = runtimes.GetLatest(ctx, "default", "local")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)

	customLocal, err := runtimes.GetLatest(ctx, "default", "custom-local")
	require.NoError(t, err)
	var customLocalSpec v1alpha1.RuntimeSpec
	require.NoError(t, json.Unmarshal(customLocal.Spec, &customLocalSpec))
	require.Equal(t, "Local", customLocalSpec.Type)

	kubernetes, err := runtimes.GetLatest(ctx, "default", "custom-kubernetes")
	require.NoError(t, err)
	var kubernetesSpec v1alpha1.RuntimeSpec
	require.NoError(t, json.Unmarshal(kubernetes.Spec, &kubernetesSpec))
	require.Equal(t, "Kubernetes", kubernetesSpec.Type)

	require.NoError(t, migrator.Migrate(12))

	restoredLocal, err := runtimes.GetLatest(ctx, "default", "local")
	require.NoError(t, err)
	var restoredLocalSpec v1alpha1.RuntimeSpec
	require.NoError(t, json.Unmarshal(restoredLocal.Spec, &restoredLocalSpec))
	require.Equal(t, "Local", restoredLocalSpec.Type)

	_, err = runtimes.GetLatest(ctx, "default", "custom-local")
	require.NoError(t, err)
}

// preExemptionControlPlaneEventFunction is the record_control_plane_event()
// variant that shipped before the Plugin/Skill resolvedSource exception was
// added to migration 009. The exception was added by editing 009 in place, so
// databases migrated before that edit still run this variant, which swallows
// ALL status-only updates. Migration 015 must converge them.
const preExemptionControlPlaneEventFunction = `
CREATE OR REPLACE FUNCTION record_control_plane_event()
RETURNS TRIGGER AS $$
DECLARE
    event_kind TEXT := TG_ARGV[0];
    event_op TEXT;
    event_revision BIGINT;
    row_json JSONB;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        IF NEW.spec = OLD.spec
           AND NEW.labels = OLD.labels
           AND NEW.annotations = OLD.annotations
           AND (
               NEW.deletion_timestamp = OLD.deletion_timestamp
               OR (NEW.deletion_timestamp IS NULL AND OLD.deletion_timestamp IS NULL)
           )
           AND COALESCE(to_jsonb(NEW)->'finalizers', '[]'::jsonb) =
               COALESCE(to_jsonb(OLD)->'finalizers', '[]'::jsonb) THEN
            RETURN NEW;
        END IF;
        event_op := 'update';
        row_json := to_jsonb(NEW);
    ELSIF TG_OP = 'DELETE' THEN
        event_op := 'delete';
        row_json := to_jsonb(OLD);
    ELSE
        event_op := 'insert';
        row_json := to_jsonb(NEW);
    END IF;

    INSERT INTO control_plane_events (
        kind, namespace, name, tag, uid, generation, op
    ) VALUES (
        event_kind,
        row_json->>'namespace',
        row_json->>'name',
        COALESCE(row_json->>'tag', ''),
        (row_json->>'uid')::uuid,
        (row_json->>'generation')::bigint,
        event_op
    )
    RETURNING revision INTO event_revision;

    PERFORM pg_notify(
        'v1alpha1_control_plane_changed',
        json_build_object('revision', event_revision)::text
    );

    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;
`

func TestReassertControlPlaneEventFunctionRepairsStaleDatabases(t *testing.T) {
	pool, dsn := NewTestPoolWithDSN(t, adminDSN())
	ctx := context.Background()

	migrator, err := NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(13))
	_, err = pool.Exec(ctx, preExemptionControlPlaneEventFunction)
	require.NoError(t, err)

	skills := NewStore(pool, TestSchema(), "skills")
	_, err = skills.Upsert(ctx, &v1alpha1.Skill{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "docs"},
		Spec: v1alpha1.SkillSpec{
			Source: &v1alpha1.SkillSource{Repository: &v1alpha1.Repository{URL: "https://github.com/acme/docs"}},
		},
	})
	require.NoError(t, err)

	events := NewControlPlaneEventStore(pool, TestSchema())
	checkpoint, err := events.CurrentRevision(ctx)
	require.NoError(t, err)

	resolveSkillStatus := func(commit string) {
		err := skills.ApplyPatch(ctx, "default", "docs", DefaultTag(), PatchOpts{
			Status: func(current json.RawMessage) (json.RawMessage, error) {
				sk := &v1alpha1.Skill{}
				if err := sk.UnmarshalStatus(current); err != nil {
					return nil, err
				}
				sk.Status.ResolvedSource = &v1alpha1.SkillResolvedSource{Commit: commit}
				return sk.MarshalStatus()
			},
		})
		require.NoError(t, err)
	}

	// The stale function swallows the status-only resolvedSource write.
	resolveSkillStatus("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	batch, err := events.ListAfter(ctx, checkpoint, 10)
	require.NoError(t, err)
	require.Empty(t, batch, "simulated stale function should swallow the resolvedSource write")

	require.NoError(t, migrator.Up())

	resolveSkillStatus("bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	batch, err = events.ListAfter(ctx, checkpoint, 10)
	require.NoError(t, err)
	require.Len(t, batch, 1, "repaired function must emit the resolvedSource event")
	require.Equal(t, v1alpha1.KindSkill, batch[0].Key.Kind)
	require.Equal(t, "docs", batch[0].Key.Name)
}

func TestRemoveKubernetesRuntimeSeedMigrationUpgradeAndRollback(t *testing.T) {
	pool, dsn := NewTestPoolWithDSN(t, adminDSN())
	ctx := context.Background()

	migrator, err := NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(13))

	runtimes := NewMutableObjectStore(pool, TestSchema(), "runtimes")
	_, err = runtimes.GetLatest(ctx, "default", "kubernetes-default")
	require.NoError(t, err)

	require.NoError(t, migrator.Up())
	_, err = runtimes.GetLatest(ctx, "default", "kubernetes-default")
	require.ErrorIs(t, err, pkgdb.ErrNotFound)

	require.NoError(t, migrator.Migrate(13))
	restored, err := runtimes.GetLatest(ctx, "default", "kubernetes-default")
	require.NoError(t, err)
	var restoredSpec v1alpha1.RuntimeSpec
	require.NoError(t, json.Unmarshal(restored.Spec, &restoredSpec))
	require.Equal(t, "Kubernetes", restoredSpec.Type)
}

func TestRemoveKubernetesRuntimeSeedMigrationPreservesModifiedOrReferencedSeed(t *testing.T) {
	t.Run("modified", func(t *testing.T) {
		pool, dsn := NewTestPoolWithDSN(t, adminDSN())
		ctx := context.Background()

		migrator, err := NewOSSMigrator(ctx, dsn)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = migrator.Close()
		})
		require.NoError(t, migrator.Migrate(13))

		_, err = pool.Exec(ctx, `
			UPDATE runtimes
			SET annotations = '{"keep":"value"}'::jsonb
			WHERE namespace = 'default' AND name = 'kubernetes-default'
		`)
		require.NoError(t, err)
		require.NoError(t, migrator.Up())

		runtimes := NewMutableObjectStore(pool, TestSchema(), "runtimes")
		modified, err := runtimes.GetLatest(ctx, "default", "kubernetes-default")
		require.NoError(t, err)
		require.Equal(t, "value", modified.Metadata.Annotations["keep"])
	})

	t.Run("referenced", func(t *testing.T) {
		pool, dsn := NewTestPoolWithDSN(t, adminDSN())
		ctx := context.Background()

		migrator, err := NewOSSMigrator(ctx, dsn)
		require.NoError(t, err)
		t.Cleanup(func() {
			_, _ = migrator.Close()
		})
		require.NoError(t, migrator.Migrate(13))

		_, err = pool.Exec(ctx, `
			INSERT INTO deployments (namespace, name, spec)
			VALUES (
				'default',
				'uses-kubernetes-default',
				'{"targetRef":{"kind":"Agent","name":"test"},"runtimeRef":{"kind":"Runtime","name":"kubernetes-default"}}'::jsonb
			)
		`)
		require.NoError(t, err)
		require.NoError(t, migrator.Up())

		runtimes := NewMutableObjectStore(pool, TestSchema(), "runtimes")
		_, err = runtimes.GetLatest(ctx, "default", "kubernetes-default")
		require.NoError(t, err)
	})
}

func TestRemoveLocalRuntimeSeedMigrationPreservesModifiedSeed(t *testing.T) {
	pool, dsn := NewTestPoolWithDSN(t, adminDSN())
	ctx := context.Background()

	migrator, err := NewOSSMigrator(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = migrator.Close()
	})
	require.NoError(t, migrator.Migrate(12))

	_, err = pool.Exec(ctx, `
		UPDATE runtimes
		SET annotations = '{"keep":"value"}'::jsonb
		WHERE namespace = 'default' AND name = 'local'
	`)
	require.NoError(t, err)

	require.NoError(t, migrator.Up())

	runtimes := NewMutableObjectStore(pool, TestSchema(), "runtimes")
	modifiedLocal, err := runtimes.GetLatest(ctx, "default", "local")
	require.NoError(t, err)
	require.Equal(t, "value", modifiedLocal.Metadata.Annotations["keep"])

	require.NoError(t, migrator.Migrate(12))

	modifiedLocal, err = runtimes.GetLatest(ctx, "default", "local")
	require.NoError(t, err)
	require.Equal(t, "value", modifiedLocal.Metadata.Annotations["keep"])
}

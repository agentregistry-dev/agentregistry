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
	require.Equal(t, v1alpha1.TypeKubernetes, kubernetesSpec.Type)

	require.NoError(t, migrator.Migrate(12))

	restoredLocal, err := runtimes.GetLatest(ctx, "default", "local")
	require.NoError(t, err)
	var restoredLocalSpec v1alpha1.RuntimeSpec
	require.NoError(t, json.Unmarshal(restoredLocal.Spec, &restoredLocalSpec))
	require.Equal(t, "Local", restoredLocalSpec.Type)

	_, err = runtimes.GetLatest(ctx, "default", "custom-local")
	require.NoError(t, err)
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

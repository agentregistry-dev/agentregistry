package v1alpha1store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

func TestInternalMetaPatchRejectsNil(t *testing.T) {
	store := &Store{}

	require.Error(t, store.PatchInternalMeta(context.Background(), "default", "deployment", nil))
	require.Error(t, store.PatchStatusAndMeta(context.Background(), "default", "deployment", nil, nil))
	require.Error(t, store.PatchStatusAndMeta(context.Background(), "default", "deployment", nil, struct{}{}))
}

func TestWithInternalMeta(t *testing.T) {
	schema, err := pkgdb.NewSchema("test")
	require.NoError(t, err)
	require.True(t, NewMutableObjectStore(nil, schema, "deployments", WithInternalMeta()).internalMeta)
	require.False(t, NewMutableObjectStore(nil, schema, "deployments").internalMeta)
}

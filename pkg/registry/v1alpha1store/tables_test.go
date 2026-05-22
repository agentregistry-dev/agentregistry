package v1alpha1store

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestNewStoreForKindUsesRegisteredDescriptor(t *testing.T) {
	agent, err := NewStoreForKind(nil, v1alpha1.KindAgent)
	require.NoError(t, err)
	require.Equal(t, TaggedArtifactStore, agent.Behavior())
	require.Equal(t, "v1alpha1.agents", agent.table)

	deployment, err := NewStoreForKind(nil, v1alpha1.KindDeployment)
	require.NoError(t, err)
	require.Equal(t, MutableObjectStore, deployment.Behavior())
	require.Equal(t, "v1alpha1.deployments", deployment.table)
}

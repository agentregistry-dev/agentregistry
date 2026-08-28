//go:build integration

package kagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestKagentDeploymentFinderMatchesManagedTargetAndRuntime(t *testing.T) {
	stores := v1alpha1store.NewStores(
		v1alpha1store.NewTestPool(t),
		v1alpha1store.TestSchemaRegistry(),
	)
	_, err := stores[v1alpha1.KindDeployment].Upsert(
		context.Background(),
		&v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{
				Namespace: "catalog",
				Name:      "discovered-tools",
				Annotations: map[string]string{
					v1alpha1.DeploymentOriginAnnotation: v1alpha1.DeploymentOriginDiscovered,
				},
			},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{
					Kind: v1alpha1.KindMCPServer,
					Name: "tools",
					Tag:  "stable",
				},
				RuntimeRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindRuntime,
					Namespace: "platform",
					Name:      "kagent-prod",
				},
			},
		},
	)
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindDeployment].Upsert(
		context.Background(),
		&v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Namespace: "deployments", Name: "wrong-tag"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindMCPServer,
					Namespace: "catalog",
					Name:      "tools",
					Tag:       "canary",
				},
				RuntimeRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindRuntime,
					Namespace: "platform",
					Name:      "kagent-prod",
				},
			},
		},
	)
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindDeployment].Upsert(
		context.Background(),
		&v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Namespace: "deployments", Name: "wrong-runtime-namespace"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindMCPServer,
					Namespace: "catalog",
					Name:      "tools",
					Tag:       "stable",
				},
				RuntimeRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindRuntime,
					Namespace: "staging",
					Name:      "kagent-prod",
				},
			},
		},
	)
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindDeployment].Upsert(
		context.Background(),
		&v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Namespace: "other", Name: "wrong-target-namespace"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{
					Kind: v1alpha1.KindMCPServer,
					Name: "tools",
					Tag:  "stable",
				},
				RuntimeRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindRuntime,
					Namespace: "platform",
					Name:      "kagent-prod",
				},
			},
		},
	)
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindDeployment].Upsert(
		context.Background(),
		&v1alpha1.Deployment{
			Metadata: v1alpha1.ObjectMeta{Namespace: "deployments", Name: "tools-prod"},
			Spec: v1alpha1.DeploymentSpec{
				TargetRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindMCPServer,
					Namespace: "catalog",
					Name:      "tools",
					Tag:       "stable",
				},
				RuntimeRef: v1alpha1.ResourceRef{
					Kind:      v1alpha1.KindRuntime,
					Namespace: "platform",
					Name:      "kagent-prod",
				},
			},
		},
	)
	require.NoError(t, err)

	deployment, found, err := NewStoreDeploymentFinder(stores[v1alpha1.KindDeployment])(
		context.Background(),
		v1alpha1.ResourceRef{
			Kind:      v1alpha1.KindMCPServer,
			Namespace: "catalog",
			Name:      "tools",
			Tag:       "stable",
		},
		v1alpha1.ResourceRef{
			Kind:      v1alpha1.KindRuntime,
			Namespace: "platform",
			Name:      "kagent-prod",
		},
	)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "tools-prod", deployment.Metadata.Name)

	_, found, err = NewStoreDeploymentFinder(stores[v1alpha1.KindDeployment])(
		context.Background(),
		v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Namespace: "catalog", Name: "tools", Tag: "stable"},
		v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Namespace: "missing", Name: "kagent-prod"},
	)
	require.NoError(t, err)
	require.False(t, found)
}

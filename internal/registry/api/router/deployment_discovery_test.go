//go:build integration

package router

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const discoveryTestRuntimeType = "DiscoveryTest"

func TestDeploymentListMergesDiscoveredRows(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	ctx := t.Context()

	_, err := stores[v1alpha1.KindRuntime].Upsert(ctx, &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "field-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: discoveryTestRuntimeType},
	})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindDeployment].Upsert(ctx, &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "managed-agent"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: "managed-agent", Tag: "latest"},
			RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "field-runtime"},
		},
	})
	require.NoError(t, err)

	adapter := &fieldDiscoveryAdapter{
		results: []types.DiscoveryResult{
			{
				TargetKind: v1alpha1.KindAgent,
				Name:       "managed-agent",
				Tag:        "latest",
				RuntimeMetadata: map[string]string{
					"remoteId": "managed-remote",
				},
			},
			{
				TargetKind: v1alpha1.KindAgent,
				Name:       "unmanaged-agent",
				RuntimeMetadata: map[string]string{
					"remoteId": "unmanaged-remote",
				},
			},
		},
	}
	resolver := deploymentsvc.NewAdapterResolver(deploymentsvc.ResolverDependencies{
		Adapters: map[string]types.DeploymentAdapter{discoveryTestRuntimeType: adapter},
		Getter:   database.NewGetter(stores),
	})

	_, api := humatest.New(t)
	registerKindRoutes(
		api,
		"/v0",
		stores,
		nil,
		resolver,
		crud.PerKindHooks{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	all := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments")
	require.Len(t, all, 2, "managed row plus one unmanaged discovered row; matching managed discovery must dedupe")
	discovered := onlyDiscoveredDeployment(t, all)
	require.Equal(t, "unmanaged-agent", discovered.Spec.TargetRef.Name)
	require.Equal(t, "field-runtime", discovered.Spec.RuntimeRef.Name)
	require.Equal(t, "unknown", discovered.Spec.TargetRef.Tag)
	require.Equal(t, v1alpha1.ConditionTrue, discovered.Status.GetCondition("Ready").Status)

	onlyDiscovered := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered")
	require.Len(t, onlyDiscovered, 1)
	require.Equal(t, discovered.Metadata.Name, onlyDiscovered[0].Metadata.Name)

	onlyManaged := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=managed")
	require.Len(t, onlyManaged, 1)
	require.Equal(t, "managed-agent", onlyManaged[0].Metadata.Name)
	require.Empty(t, onlyManaged[0].Metadata.Annotations[deploymentOriginAnnotation])
}

func listDeploymentsForDiscoveryTest(t *testing.T, api humatest.TestAPI, path string) []v1alpha1.Deployment {
	t.Helper()
	resp := api.Get(path)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out struct {
		Items []v1alpha1.Deployment `json:"items"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	return out.Items
}

func onlyDiscoveredDeployment(t *testing.T, deployments []v1alpha1.Deployment) v1alpha1.Deployment {
	t.Helper()
	var found []v1alpha1.Deployment
	for _, deployment := range deployments {
		if deployment.Metadata.Annotations[deploymentOriginAnnotation] == "discovered" {
			found = append(found, deployment)
		}
	}
	require.Len(t, found, 1)
	return found[0]
}

type fieldDiscoveryAdapter struct {
	results []types.DiscoveryResult
}

func (a *fieldDiscoveryAdapter) Type() string { return discoveryTestRuntimeType }

func (a *fieldDiscoveryAdapter) SupportedTargetKinds() []string {
	return []string{v1alpha1.KindAgent, v1alpha1.KindMCPServer}
}

func (a *fieldDiscoveryAdapter) Apply(context.Context, types.ApplyInput) (*types.ApplyResult, error) {
	return &types.ApplyResult{}, nil
}

func (a *fieldDiscoveryAdapter) Remove(context.Context, types.RemoveInput) (*types.RemoveResult, error) {
	return &types.RemoveResult{}, nil
}

func (a *fieldDiscoveryAdapter) Logs(context.Context, types.LogsInput) (<-chan types.LogLine, error) {
	ch := make(chan types.LogLine)
	close(ch)
	return ch, nil
}

func (a *fieldDiscoveryAdapter) Discover(context.Context, types.DiscoverInput) ([]types.DiscoveryResult, error) {
	return a.results, nil
}

//go:build integration

package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

func TestDeploymentListFiltersPersistedDiscoveredRows(t *testing.T) {
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool, v1alpha1store.TestSchemaRegistry())
	ctx := t.Context()

	seedDeploymentForDiscoveryListTest(t, stores, "managed-agent", nil, map[string]string{"tier": "managed"})
	seedDeploymentForDiscoveryListTest(t, stores, "discovered-alpha", map[string]string{
		v1alpha1.DeploymentOriginAnnotation: v1alpha1.DeploymentOriginDiscovered,
	}, map[string]string{"tier": "discovered"})
	seedDeploymentForDiscoveryListTest(t, stores, "discovered-beta", map[string]string{
		v1alpha1.DeploymentOriginAnnotation: v1alpha1.DeploymentOriginDiscovered,
	}, map[string]string{"tier": "discovered"})

	_, err := stores[v1alpha1.KindRuntime].Upsert(ctx, &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "field-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: "DiscoveryTest"},
	})
	require.NoError(t, err)

	_, api := humatest.New(t)
	registerKindRoutes(
		api,
		"/v0",
		stores,
		nil,
		crud.PerKindHooks{},
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	all := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments")
	require.Len(t, all.Items, 3)

	onlyDiscovered := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered")
	require.Len(t, onlyDiscovered.Items, 2)
	require.Empty(t, onlyDiscovered.NextCursor)
	for _, deployment := range onlyDiscovered.Items {
		require.Equal(t, v1alpha1.DeploymentOriginDiscovered, deployment.Metadata.Annotations[v1alpha1.DeploymentOriginAnnotation])
	}

	onlyManaged := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=managed")
	require.Len(t, onlyManaged.Items, 1)
	require.Equal(t, "managed-agent", onlyManaged.Items[0].Metadata.Name)
	require.Empty(t, onlyManaged.Items[0].Metadata.Annotations[v1alpha1.DeploymentOriginAnnotation])

	labeled := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered&labels=tier=discovered")
	require.Len(t, labeled.Items, 2)
	none := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered&labels=tier=managed")
	require.Empty(t, none.Items)

	page1 := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered&limit=1")
	require.Len(t, page1.Items, 1)
	require.NotEmpty(t, page1.NextCursor)
	page2 := listDeploymentsForDiscoveryTest(t, api, "/v0/deployments?origin=discovered&limit=1&cursor="+page1.NextCursor)
	require.Len(t, page2.Items, 1)
	require.Empty(t, page2.NextCursor)
	require.NotEqual(t, page1.Items[0].Metadata.Name, page2.Items[0].Metadata.Name)
}

func seedDeploymentForDiscoveryListTest(
	t *testing.T,
	stores map[string]*v1alpha1store.Store,
	name string,
	annotations map[string]string,
	labels map[string]string,
) {
	t.Helper()
	_, err := stores[v1alpha1.KindDeployment].Upsert(t.Context(), &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{
			Namespace:   "default",
			Name:        name,
			Annotations: annotations,
			Labels:      labels,
		},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindAgent, Name: name, Tag: "latest"},
			RuntimeRef: v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "field-runtime"},
		},
	})
	require.NoError(t, err)
}

type deploymentListResponse struct {
	Items      []v1alpha1.Deployment `json:"items"`
	NextCursor string                `json:"nextCursor"`
}

func listDeploymentsForDiscoveryTest(t *testing.T, api humatest.TestAPI, path string) deploymentListResponse {
	t.Helper()
	resp := api.Get(path)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var out deploymentListResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &out))
	return out
}

//go:build integration

package crud_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/controller"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/runtimes/noop"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// seedDeploymentFixtures prepares the DB with a noop Runtime + MCPServer
// so a Deployment PUT has refs to resolve. Returns the wired-up humatest
// API + the underlying stores for assertions.
func seedDeploymentFixtures(t *testing.T) (humatest.TestAPI, map[string]*v1alpha1store.Store) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	ctx := t.Context()

	_, err := stores[v1alpha1.KindMCPServer].Upsert(ctx, &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather"},
		Spec: v1alpha1.MCPServerSpec{
			Description: "noop server",
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: v1alpha1.RegistryTypeOCI,
					Identifier:   "ghcr.io/example/weather:1.0.0",
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	})
	require.NoError(t, err)

	_, err = stores[v1alpha1.KindRuntime].Upsert(ctx, &v1alpha1.Runtime{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "noop-runtime"},
		Spec:     v1alpha1.RuntimeSpec{Type: noop.RuntimeType},
	})
	require.NoError(t, err)

	resolver := deploymentsvc.NewAdapterResolver(deploymentsvc.ResolverDependencies{
		Adapters: map[string]types.DeploymentAdapter{noop.RuntimeType: noop.New()},
		Getter:   database.NewGetter(stores),
	})

	_, api := humatest.New(t)
	crud.Register(
		api, "/v0", stores,
		database.NewResolver(stores),
		nil, // registryValidator
		crud.PerKindHooks{
			InitialFinalizers: map[string]func(v1alpha1.Object) []string{
				v1alpha1.KindDeployment: func(v1alpha1.Object) []string {
					return []string{controller.DeploymentControllerFinalizer}
				},
			},
		},
		nil,
	)
	deploymentlogs.Register(api, deploymentlogs.Config{
		BasePrefix:  "/v0",
		Store:       stores[v1alpha1.KindDeployment],
		LogResolver: resolver,
	})
	return api, stores
}

func TestDeploymentPut_PersistsWithoutSynchronousAdapterApply(t *testing.T) {
	api, stores := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "noop-runtime"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	resp := api.Put("/v0/deployments/weather-noop", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Adapter status is no longer written during the API call; the
	// Deployment controller patches it asynchronously.
	var got v1alpha1.Deployment
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.Empty(t, got.Status.Conditions, "adapter status is written asynchronously by the Deployment controller")

	raw, err := stores[v1alpha1.KindDeployment].Get(t.Context(), "default", "weather-noop", "")
	require.NoError(t, err)
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	require.Nil(t, status.GetCondition("Ready"))
}

func TestDeploymentDelete_LeavesTerminatingRowForControllerRemove(t *testing.T) {
	api, stores := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "noop-runtime"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	putResp := api.Put("/v0/deployments/weather-noop", body)
	require.Equal(t, http.StatusOK, putResp.Code, putResp.Body.String())

	delResp := api.Delete("/v0/deployments/weather-noop")
	require.Equal(t, http.StatusNoContent, delResp.Code, delResp.Body.String())

	raw, err := stores[v1alpha1.KindDeployment].GetLatestIncludingTerminating(t.Context(), "default", "weather-noop")
	require.NoError(t, err)
	require.NotNil(t, raw.Metadata.DeletionTimestamp)
}

func TestDeploymentLogs_EmptyForNoopAdapter(t *testing.T) {
	api, _ := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Tag: v1alpha1store.DefaultTag()},
			RuntimeRef:   v1alpha1.ResourceRef{Kind: v1alpha1.KindRuntime, Name: "noop-runtime"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	require.Equal(t, http.StatusOK, api.Put("/v0/deployments/weather-noop", body).Code)

	resp := api.Get("/v0/deployments/weather-noop/logs")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body2 struct {
		Lines []struct {
			Line string `json:"line"`
		} `json:"lines"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body2))
	require.Empty(t, body2.Lines, "noop adapter returns closed channel; logs payload must be empty")
}

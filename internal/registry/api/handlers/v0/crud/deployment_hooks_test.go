//go:build integration

package crud_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/danielgtaylor/huma/v2/humatest"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentlogs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/noop"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// seedDeploymentFixtures prepares the DB with a noop Provider + MCPServer
// so a Deployment PUT has refs to resolve. Returns the wired-up humatest
// API + the underlying stores for assertions.
func seedDeploymentFixtures(t *testing.T) (humatest.TestAPI, map[string]*v1alpha1store.Store) {
	t.Helper()
	pool := v1alpha1store.NewTestPool(t)
	stores := v1alpha1store.NewStores(pool)
	ctx := t.Context()

	mcpSpec, err := json.Marshal(v1alpha1.MCPServerSpec{
		Description: "noop server",
		Packages: []v1alpha1.MCPPackage{{
			RegistryType: v1alpha1.RegistryTypeOCI,
			Identifier:   "ghcr.io/example/weather:1.0.0",
			Transport:    v1alpha1.MCPTransport{Type: "stdio"},
		}},
	})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindMCPServer].Upsert(ctx, "default", "weather", "1.0.0", mcpSpec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	providerSpec, err := json.Marshal(v1alpha1.ProviderSpec{Platform: noop.Platform})
	require.NoError(t, err)
	_, err = stores[v1alpha1.KindProvider].Upsert(ctx, "default", "noop-provider", "1", providerSpec, v1alpha1store.UpsertOpts{})
	require.NoError(t, err)

	coord := deploymentsvc.NewCoordinator(deploymentsvc.Dependencies{
		Stores:   stores,
		Adapters: map[string]types.DeploymentAdapter{noop.Platform: noop.New()},
		Getter:   database.NewGetter(stores),
	})

	_, api := humatest.New(t)
	crud.Register(
		api, "/v0", stores,
		database.NewResolver(stores),
		nil, // registryValidator
		crud.PerKindHooks{
			PostUpserts: map[string]func(context.Context, v1alpha1.Object) error{
				v1alpha1.KindDeployment: func(ctx context.Context, obj v1alpha1.Object) error {
					return coord.Apply(ctx, obj.(*v1alpha1.Deployment))
				},
			},
			PostDeletes: map[string]func(context.Context, v1alpha1.Object) error{
				v1alpha1.KindDeployment: func(ctx context.Context, obj v1alpha1.Object) error {
					return coord.Remove(ctx, obj.(*v1alpha1.Deployment))
				},
			},
		},
	)
	deploymentlogs.Register(api, deploymentlogs.Config{
		BasePrefix:  "/v0",
		Store:       stores[v1alpha1.KindDeployment],
		Coordinator: coord,
	})
	return api, stores
}

func TestDeploymentPut_TriggersAdapterApply(t *testing.T) {
	api, stores := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop", Version: "1"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	resp := api.Put("/v0/deployments/weather-noop/1", body)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Response should reflect the PostUpsert status writes.
	var got v1alpha1.Deployment
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &got))
	require.NotEmpty(t, got.Status.Conditions, "expected status conditions from coordinator.Apply")

	// Row in DB: status JSONB carries the Ready condition.
	raw, err := stores[v1alpha1.KindDeployment].Get(t.Context(), "default", "weather-noop", "1")
	require.NoError(t, err)
	// RawObject.Status is opaque bytes at the envelope layer; decode
	// with the Status storage codec to inspect conditions.
	var status v1alpha1.Status
	require.NoError(t, v1alpha1.UnmarshalStatusFromStorage(raw.Status, &status))
	ready := status.GetCondition("Ready")
	require.NotNil(t, ready, "noop.Apply should have written Ready condition")
	require.Equal(t, v1alpha1.ConditionTrue, ready.Status)
}

func TestDeploymentDelete_TriggersAdapterRemove(t *testing.T) {
	api, stores := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop", Version: "1"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	putResp := api.Put("/v0/deployments/weather-noop/1", body)
	require.Equal(t, http.StatusOK, putResp.Code, putResp.Body.String())

	delResp := api.Delete("/v0/deployments/weather-noop/1")
	require.Equal(t, http.StatusNoContent, delResp.Code, delResp.Body.String())

	// Deployment carries no finalizers, so DELETE hard-deletes the row
	// after Coordinator.Remove fires the adapter teardown. Re-apply
	// with the same identity then succeeds without an ErrTerminating
	// race — see commit fixing josh-pritchard's PR #455 report
	// "Soft-delete blocks re-apply for every v1alpha1 kind."
	_, err := stores[v1alpha1.KindDeployment].Get(t.Context(), "default", "weather-noop", "1")
	require.ErrorIs(t, err, pkgdb.ErrNotFound, "finalizer-free row must hard-delete")
}

func TestDeploymentLogs_EmptyForNoopAdapter(t *testing.T) {
	api, _ := seedDeploymentFixtures(t)

	body := v1alpha1.Deployment{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: v1alpha1.KindDeployment},
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "weather-noop", Version: "1"},
		Spec: v1alpha1.DeploymentSpec{
			TargetRef:    v1alpha1.ResourceRef{Kind: v1alpha1.KindMCPServer, Name: "weather", Version: "1.0.0"},
			ProviderRef:  v1alpha1.ResourceRef{Kind: v1alpha1.KindProvider, Name: "noop-provider", Version: "1"},
			DesiredState: v1alpha1.DesiredStateDeployed,
		},
	}
	require.Equal(t, http.StatusOK, api.Put("/v0/deployments/weather-noop/1", body).Code)

	resp := api.Get("/v0/deployments/weather-noop/1/logs")
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	var body2 struct {
		Lines []struct {
			Line string `json:"line"`
		} `json:"lines"`
	}
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body2))
	require.Empty(t, body2.Lines, "noop adapter returns closed channel; logs payload must be empty")
}

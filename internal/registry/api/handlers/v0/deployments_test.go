package v0_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDeploymentAdapter struct {
	deployCalled   bool
	deployErr      error
	undeployErr    error
	getLogsErr     error
	cancelErr      error
	undeployCalled bool
	getLogsCalled  bool
	cancelCalled   bool
	lastDeployReq  *models.Deployment
}

func (f *fakeDeploymentAdapter) Platform() string { return "local" }
func (f *fakeDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}
func (f *fakeDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.Deployment, error) {
	f.deployCalled = true
	f.lastDeployReq = req
	if f.deployErr != nil {
		return nil, f.deployErr
	}
	return &models.Deployment{
		ID:           "adapter-dep-1",
		ServerName:   req.ServerName,
		Version:      req.Version,
		ResourceType: req.ResourceType,
		ProviderID:   req.ProviderID,
		Status:       "deployed",
		Origin:       "managed",
		Env:          req.Env,
	}, nil
}

func TestCreateDeployment_PassesEnvAndProviderConfigSeparately(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	reg.CreateDeploymentFn = func(ctx context.Context, req *models.Deployment, platform string) (*models.Deployment, error) {
		return adapter.Deploy(ctx, req)
	}

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
		"env": map[string]string{
			"API_KEY": "abc",
		},
		"providerConfig": map[string]any{
			"securityGroupId": "sg-123",
		},
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, adapter.deployCalled)
	require.NotNil(t, adapter.lastDeployReq)
	assert.Equal(t, "abc", adapter.lastDeployReq.Env["API_KEY"])
	var providerCfg map[string]string
	err = adapter.lastDeployReq.ProviderConfig.UnmarshalInto(&providerCfg)
	require.NoError(t, err)
	assert.Equal(t, "sg-123", providerCfg["securityGroupId"])
}

func (f *fakeDeploymentAdapter) Undeploy(_ context.Context, _ *models.Deployment) error {
	f.undeployCalled = true
	return f.undeployErr
}
func (f *fakeDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	f.getLogsCalled = true
	if f.getLogsErr != nil {
		return nil, f.getLogsErr
	}
	return []string{"line-1", "line-2"}, nil
}
func (f *fakeDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	f.cancelCalled = true
	return f.cancelErr
}
func (f *fakeDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return []*models.Deployment{}, nil
}

func TestDeleteDeployment_DiscoveredReturnsConflict(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Origin:     "discovered",
		}, nil
	}
	reg.RemoveDeploymentByIDFn = func(ctx context.Context, id string) error {
		return database.ErrInvalidInput
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	adapter := &fakeDeploymentAdapter{undeployErr: database.ErrInvalidInput}
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/v0/deployments/dep-discovered-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "Discovered deployments cannot be deleted directly")
}

func TestCreateDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}
	reg.DeployServerFn = func(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
		t.Fatalf("expected adapter dispatch, but DeployServer was called")
		return nil, nil
	}

	adapter := &fakeDeploymentAdapter{}
	reg.CreateDeploymentFn = func(ctx context.Context, req *models.Deployment, platform string) (*models.Deployment, error) {
		return adapter.Deploy(ctx, req)
	}

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.True(t, adapter.deployCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	var got models.Deployment
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "io.github.user/weather", got.ServerName)
}

func TestCreateDeployment_InvalidInputFromAdapterReturnsBadRequest(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}
	adapter := &fakeDeploymentAdapter{deployErr: database.ErrInvalidInput}
	reg.CreateDeploymentFn = func(ctx context.Context, req *models.Deployment, platform string) (*models.Deployment, error) {
		return adapter.Deploy(ctx, req)
	}

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDeleteDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}
	reg.RemoveDeploymentByIDFn = func(ctx context.Context, id string) error {
		t.Fatalf("expected adapter undeploy, but RemoveDeploymentByID was called")
		return nil
	}

	adapter := &fakeDeploymentAdapter{}
	reg.UndeployDeploymentFn = func(ctx context.Context, deployment *models.Deployment, platform string) error {
		return adapter.Undeploy(ctx, deployment)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodDelete, "/v0/deployments/dep-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, adapter.undeployCalled)
}

func TestGetDeploymentLogs_UsesAdapterWhenRegistered(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	reg.GetDeploymentLogsFn = func(ctx context.Context, deployment *models.Deployment, platform string) ([]string, error) {
		return adapter.GetLogs(ctx, deployment)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/deployments/dep-2/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, adapter.getLogsCalled)
}

func TestGetDeploymentLogs_NotFoundFromAdapterReturnsNotFound(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{getLogsErr: database.ErrNotFound}
	reg.GetDeploymentLogsFn = func(ctx context.Context, deployment *models.Deployment, platform string) ([]string, error) {
		return adapter.GetLogs(ctx, deployment)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodGet, "/v0/deployments/dep-2/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.True(t, adapter.getLogsCalled)
}

func TestCancelDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deploying",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	reg.CancelDeploymentFn = func(ctx context.Context, deployment *models.Deployment, platform string) error {
		return adapter.Cancel(ctx, deployment)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments/dep-3/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, adapter.cancelCalled)
}

func TestCancelDeployment_InvalidInputFromAdapterReturnsBadRequest(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deploying",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{cancelErr: database.ErrInvalidInput}
	reg.CancelDeploymentFn = func(ctx context.Context, deployment *models.Deployment, platform string) error {
		return adapter.Cancel(ctx, deployment)
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0.RegisterDeploymentsEndpoints(api, "/v0", reg, v0.PlatformExtensions{
		ProviderPlatforms: v0.DefaultProviderPlatformAdapters(reg),
		DeploymentPlatforms: map[string]registrytypes.DeploymentPlatformAdapter{
			"local": adapter,
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments/dep-3/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, adapter.cancelCalled)
}

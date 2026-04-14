package providers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	v0providers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/providers"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeProviderService struct {
	listProvidersFn func(ctx context.Context, platform string) ([]*models.Provider, error)
	getProviderFn   func(ctx context.Context, providerID string) (*models.Provider, error)
	resolveFn       func(ctx context.Context, providerID, platformHint string) (*models.Provider, error)
	createFn        func(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	updateFn        func(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error)
	deleteFn        func(ctx context.Context, providerID, platformHint string) error
}

func (f *fakeProviderService) ListProviders(ctx context.Context, platform string) ([]*models.Provider, error) {
	if f.listProvidersFn != nil {
		return f.listProvidersFn(ctx, platform)
	}
	return []*models.Provider{}, nil
}

func (f *fakeProviderService) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.getProviderFn != nil {
		return f.getProviderFn(ctx, providerID)
	}
	return nil, database.ErrNotFound
}

func (f *fakeProviderService) ResolveProvider(ctx context.Context, providerID, platformHint string) (*models.Provider, error) {
	if f.resolveFn != nil {
		return f.resolveFn(ctx, providerID, platformHint)
	}
	return f.GetProvider(ctx, providerID)
}

func (f *fakeProviderService) RegisterProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	if f.createFn != nil {
		return f.createFn(ctx, in)
	}
	return nil, database.ErrNotFound
}

func (f *fakeProviderService) UpdateProvider(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error) {
	if f.updateFn != nil {
		return f.updateFn(ctx, providerID, platformHint, in)
	}
	return nil, database.ErrNotFound
}

func (f *fakeProviderService) DeleteProvider(ctx context.Context, providerID, platformHint string) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, providerID, platformHint)
	}
	return database.ErrNotFound
}

func (f *fakeProviderService) PlatformAdapters() map[string]types.ProviderPlatformAdapter {
	return nil
}

func TestListProviders_EmptyReturnsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0providers.RegisterProvidersEndpoints(api, "/v0", &fakeProviderService{})

	req := httptest.NewRequest(http.MethodGet, "/v0/providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"providers":[]`)
	assert.Contains(t, w.Body.String(), `"count":0`)
}

func TestCreateAndGetProvider(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	fake := &fakeProviderService{}

	fake.createFn = func(_ context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
		return &models.Provider{
			ID:       "kubernetes-1",
			Name:     in.Name,
			Platform: in.Platform,
			Config:   in.Config,
		}, nil
	}
	fake.resolveFn = func(_ context.Context, providerID, _ string) (*models.Provider, error) {
		if providerID != "kubernetes-1" {
			return nil, database.ErrNotFound
		}
		return &models.Provider{ID: "kubernetes-1", Name: "prod-account", Platform: "kubernetes"}, nil
	}
	v0providers.RegisterProvidersEndpoints(api, "/v0", fake)

	createBody := map[string]any{
		"name":     "prod-account",
		"platform": "kubernetes",
		"config": map[string]any{
			"region": "us-east-1",
		},
	}
	payload, err := json.Marshal(createBody)
	require.NoError(t, err)

	createReq := httptest.NewRequest(http.MethodPost, "/v0/providers", bytes.NewReader(payload))
	createReq.Header.Set("Content-Type", "application/json")
	createW := httptest.NewRecorder()
	mux.ServeHTTP(createW, createReq)

	require.Equal(t, http.StatusOK, createW.Code)
	assert.Contains(t, createW.Body.String(), `"platform":"kubernetes"`)
	assert.Contains(t, createW.Body.String(), `"name":"prod-account"`)
	assert.Contains(t, createW.Body.String(), `"id":"kubernetes-1"`)

	getReq := httptest.NewRequest(http.MethodGet, "/v0/providers/kubernetes-1", nil)
	getW := httptest.NewRecorder()
	mux.ServeHTTP(getW, getReq)

	require.Equal(t, http.StatusOK, getW.Code)
	assert.Contains(t, getW.Body.String(), `"id":"kubernetes-1"`)
	assert.Contains(t, getW.Body.String(), `"platform":"kubernetes"`)
}

func TestListProviders_WithData(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	fake := &fakeProviderService{
		listProvidersFn: func(context.Context, string) ([]*models.Provider, error) {
			return []*models.Provider{
				{ID: "local", Name: "Local", Platform: "local"},
				{ID: "kubernetes-default", Name: "Kubernetes Default", Platform: "kubernetes"},
			}, nil
		},
	}
	v0providers.RegisterProvidersEndpoints(api, "/v0", fake)

	req := httptest.NewRequest(http.MethodGet, "/v0/providers", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"id":"local"`)
	assert.Contains(t, w.Body.String(), `"platform":"local"`)
	assert.Contains(t, w.Body.String(), `"id":"kubernetes-default"`)
	assert.Contains(t, w.Body.String(), `"platform":"kubernetes"`)
}

func TestDeleteProvider_NotFound(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	fake := &fakeProviderService{
		deleteFn: func(_ context.Context, providerID, _ string) error {
			return database.ErrNotFound
		},
	}
	v0providers.RegisterProvidersEndpoints(api, "/v0", fake)
	req := httptest.NewRequest(http.MethodDelete, "/v0/providers/missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

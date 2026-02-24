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

type fakeProviderAdapter struct {
	platform  string
	providers map[string]*models.Provider
}

func (f *fakeProviderAdapter) Platform() string { return f.platform }

func (f *fakeProviderAdapter) ListProviders(_ context.Context) ([]*models.Provider, error) {
	out := make([]*models.Provider, 0, len(f.providers))
	for _, p := range f.providers {
		out = append(out, p)
	}
	return out, nil
}

func (f *fakeProviderAdapter) CreateProvider(_ context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	p := &models.Provider{ID: "kubernetes-1", Name: in.Name, Platform: in.Platform, Config: in.Config}
	if in.ID != "" {
		p.ID = in.ID
	}
	f.providers[p.ID] = p
	return p, nil
}

func (f *fakeProviderAdapter) GetProvider(_ context.Context, providerID string) (*models.Provider, error) {
	p, ok := f.providers[providerID]
	if !ok {
		return nil, database.ErrNotFound
	}
	return p, nil
}

func (f *fakeProviderAdapter) UpdateProvider(_ context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	p, ok := f.providers[providerID]
	if !ok {
		return nil, database.ErrNotFound
	}
	if in.Name != nil {
		p.Name = *in.Name
	}
	if in.Config != nil {
		p.Config = in.Config
	}
	return p, nil
}

func (f *fakeProviderAdapter) DeleteProvider(_ context.Context, providerID string) error {
	if _, ok := f.providers[providerID]; !ok {
		return database.ErrNotFound
	}
	delete(f.providers, providerID)
	return nil
}

func TestListProviders_EmptyReturnsEmpty(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	fake := servicetesting.NewFakeRegistry()
	kubernetesAdapter := &fakeProviderAdapter{platform: "kubernetes", providers: map[string]*models.Provider{}}
	v0.RegisterProvidersEndpoints(api, "/v0", fake, v0.PlatformExtensions{
		ProviderPlatforms: map[string]registrytypes.ProviderPlatformAdapter{
			"kubernetes": kubernetesAdapter,
		},
	})

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
	fake := servicetesting.NewFakeRegistry()

	fake.CreateProviderFn = func(_ context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
		return &models.Provider{
			ID:       "kubernetes-1",
			Name:     in.Name,
			Platform: in.Platform,
			Config:   in.Config,
		}, nil
	}
	fake.GetProviderByIDFn = func(_ context.Context, providerID string) (*models.Provider, error) {
		if providerID != "kubernetes-1" {
			return nil, database.ErrNotFound
		}
		return &models.Provider{ID: "kubernetes-1", Name: "prod-account", Platform: "kubernetes"}, nil
	}
	kubernetesAdapter := &fakeProviderAdapter{platform: "kubernetes", providers: map[string]*models.Provider{}}
	v0.RegisterProvidersEndpoints(api, "/v0", fake, v0.PlatformExtensions{
		ProviderPlatforms: map[string]registrytypes.ProviderPlatformAdapter{
			"kubernetes": kubernetesAdapter,
		},
	})

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
	fake := servicetesting.NewFakeRegistry()
	localAdapter := &fakeProviderAdapter{
		platform: "local",
		providers: map[string]*models.Provider{
			"local": {ID: "local", Name: "Local", Platform: "local"},
		},
	}
	k8sAdapter := &fakeProviderAdapter{
		platform: "kubernetes",
		providers: map[string]*models.Provider{
			"kubernetes-default": {ID: "kubernetes-default", Name: "Kubernetes Default", Platform: "kubernetes"},
		},
	}
	v0.RegisterProvidersEndpoints(api, "/v0", fake, v0.PlatformExtensions{
		ProviderPlatforms: map[string]registrytypes.ProviderPlatformAdapter{
			"local":      localAdapter,
			"kubernetes": k8sAdapter,
		},
	})

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
	fake := servicetesting.NewFakeRegistry()
	fake.DeleteProviderFn = func(_ context.Context, providerID string) error {
		return database.ErrNotFound
	}
	v0.RegisterProvidersEndpoints(api, "/v0", fake, v0.PlatformExtensions{})
	req := httptest.NewRequest(http.MethodDelete, "/v0/providers/missing", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

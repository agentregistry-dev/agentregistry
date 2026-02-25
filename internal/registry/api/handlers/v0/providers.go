package v0

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

type ProviderListInput struct {
	Platform string `query:"platform" json:"platform,omitempty" doc:"Filter providers by platform type (local, kubernetes)"`
}

type ProviderByIDInput struct {
	ProviderID string `path:"providerId" json:"providerId" doc:"Provider ID"`
	Platform   string `query:"platform" json:"platform,omitempty" doc:"Provider platform hint (optional)"`
}

type CreateProviderRequest struct {
	Body models.CreateProviderInput
}

type UpdateProviderRequest struct {
	ProviderID string `path:"providerId" json:"providerId" doc:"Provider ID"`
	Platform   string `query:"platform" json:"platform,omitempty" doc:"Provider platform hint (optional)"`
	Body       models.UpdateProviderInput
}

type ProvidersListResponse struct {
	Body struct {
		Providers []models.Provider `json:"providers"`
		Count     int               `json:"count"`
	}
}

type ProviderResponse struct {
	Body models.Provider
}

func adapterPlatformKeys(extensions PlatformExtensions) []string {
	if len(extensions.ProviderPlatforms) == 0 {
		return nil
	}
	keys := make([]string, 0, len(extensions.ProviderPlatforms))
	for platform := range extensions.ProviderPlatforms {
		keys = append(keys, platform)
	}
	slices.Sort(keys)
	return keys
}

func unsupportedProviderPlatformError(platform string) error {
	p := strings.TrimSpace(platform)
	if p == "" {
		p = "unknown"
	}
	return huma.Error400BadRequest("Provider platform is not supported: " + p)
}

func getProviderByHint(ctx context.Context, extensions PlatformExtensions, providerID, platformHint string) (*models.Provider, error) {
	if strings.TrimSpace(platformHint) == "" {
		return nil, nil
	}
	platform := strings.ToLower(strings.TrimSpace(platformHint))
	adapter, ok := extensions.ResolveProviderAdapter(platform)
	if !ok {
		return nil, unsupportedProviderPlatformError(platform)
	}
	provider, err := adapter.GetProvider(ctx, providerID)
	if err == nil && provider != nil {
		return provider, nil
	}
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, huma.Error404NotFound("Provider not found")
		}
		return nil, huma.Error500InternalServerError("Failed to get provider", err)
	}
	return nil, huma.Error404NotFound("Provider not found")
}

func listProvidersForAllPlatforms(ctx context.Context, extensions PlatformExtensions) ([]*models.Provider, error) {
	out := make([]*models.Provider, 0)
	for _, platform := range adapterPlatformKeys(extensions) {
		adapter, ok := extensions.ResolveProviderAdapter(platform)
		if !ok {
			continue
		}
		providers, err := adapter.ListProviders(ctx)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to list providers", err)
		}
		out = append(out, providers...)
	}
	return out, nil
}

func listProvidersForPlatform(ctx context.Context, extensions PlatformExtensions, platform string) ([]*models.Provider, error) {
	adapter, ok := extensions.ResolveProviderAdapter(platform)
	if !ok {
		return nil, unsupportedProviderPlatformError(platform)
	}
	providers, err := adapter.ListProviders(ctx)
	if err != nil {
		return nil, huma.Error500InternalServerError("Failed to list providers", err)
	}
	return providers, nil
}

func getProviderByID(ctx context.Context, registry service.RegistryService, extensions PlatformExtensions, providerID, platformHint string) (*models.Provider, error) {
	hintedProvider, err := getProviderByHint(ctx, extensions, providerID, platformHint)
	if hintedProvider != nil || err != nil {
		return hintedProvider, err
	}

	for _, platform := range adapterPlatformKeys(extensions) {
		adapter, ok := extensions.ResolveProviderAdapter(platform)
		if !ok {
			continue
		}
		provider, err := adapter.GetProvider(ctx, providerID)
		if err == nil && provider != nil {
			return provider, nil
		}
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, huma.Error500InternalServerError("Failed to get provider", err)
		}
	}

	// Determine whether the provider exists but has an unsupported platform.
	provider, err := registry.GetProviderByID(ctx, providerID)
	if err == nil && provider != nil {
		return nil, unsupportedProviderPlatformError(provider.Platform)
	}
	if err != nil && !errors.Is(err, database.ErrNotFound) {
		return nil, huma.Error500InternalServerError("Failed to get provider", err)
	}

	return nil, huma.Error404NotFound("Provider not found")
}

// RegisterProvidersEndpoints registers provider CRUD endpoints.
func RegisterProvidersEndpoints(api huma.API, basePath string, registry service.RegistryService, extensions PlatformExtensions) {
	huma.Register(api, huma.Operation{
		OperationID: "list-providers",
		Method:      http.MethodGet,
		Path:        basePath + "/providers",
		Summary:     "List providers",
		Description: "List configured deployment target providers.",
		Tags:        []string{"providers"},
	}, func(ctx context.Context, input *ProviderListInput) (*ProvidersListResponse, error) {
		resp := &ProvidersListResponse{}
		resp.Body.Providers = []models.Provider{}

		seen := make(map[string]struct{})
		appendProvider := func(p *models.Provider) {
			if p == nil {
				return
			}
			if _, ok := seen[p.ID]; ok {
				return
			}
			seen[p.ID] = struct{}{}
			resp.Body.Providers = append(resp.Body.Providers, *p)
		}

		platform := strings.ToLower(strings.TrimSpace(input.Platform))
		var providers []*models.Provider
		var err error
		if platform != "" {
			providers, err = listProvidersForPlatform(ctx, extensions, platform)
		} else {
			providers, err = listProvidersForAllPlatforms(ctx, extensions)
		}
		if err != nil {
			return nil, err
		}
		for _, p := range providers {
			appendProvider(p)
		}
		resp.Body.Count = len(resp.Body.Providers)
		return resp, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-provider",
		Method:      http.MethodPost,
		Path:        basePath + "/providers",
		Summary:     "Create provider",
		Description: "Create a deployment target provider for a specific platform type.",
		Tags:        []string{"providers"},
	}, func(ctx context.Context, input *CreateProviderRequest) (*ProviderResponse, error) {
		if input.Body.Platform == "" {
			return nil, huma.Error400BadRequest("platform is required")
		}

		platform := strings.ToLower(strings.TrimSpace(input.Body.Platform))
		input.Body.Platform = platform
		adapter, ok := extensions.ResolveProviderAdapter(platform)
		if !ok {
			return nil, unsupportedProviderPlatformError(platform)
		}
		provider, err := adapter.CreateProvider(ctx, &input.Body)
		if err != nil {
			if errors.Is(err, database.ErrAlreadyExists) {
				return nil, huma.Error409Conflict("Provider already exists")
			}
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid provider input")
			}
			return nil, huma.Error500InternalServerError("Failed to create provider", err)
		}
		return &ProviderResponse{Body: *provider}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-provider",
		Method:      http.MethodGet,
		Path:        basePath + "/providers/{providerId}",
		Summary:     "Get provider",
		Description: "Get a provider by ID.",
		Tags:        []string{"providers"},
	}, func(ctx context.Context, input *ProviderByIDInput) (*ProviderResponse, error) {
		provider, err := getProviderByID(ctx, registry, extensions, input.ProviderID, input.Platform)
		if err != nil {
			return nil, err
		}
		return &ProviderResponse{Body: *provider}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-provider",
		Method:      http.MethodPut,
		Path:        basePath + "/providers/{providerId}",
		Summary:     "Update provider",
		Description: "Update mutable fields of a provider by ID.",
		Tags:        []string{"providers"},
	}, func(ctx context.Context, input *UpdateProviderRequest) (*ProviderResponse, error) {
		provider, err := getProviderByID(ctx, registry, extensions, input.ProviderID, input.Platform)
		if err != nil {
			return nil, err
		}

		platform := strings.ToLower(strings.TrimSpace(provider.Platform))
		adapter, ok := extensions.ResolveProviderAdapter(platform)
		if !ok {
			return nil, unsupportedProviderPlatformError(platform)
		}
		updated, err := adapter.UpdateProvider(ctx, input.ProviderID, &input.Body)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Provider not found")
			}
			return nil, huma.Error500InternalServerError("Failed to update provider", err)
		}
		return &ProviderResponse{Body: *updated}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-provider",
		Method:      http.MethodDelete,
		Path:        basePath + "/providers/{providerId}",
		Summary:     "Delete provider",
		Description: "Delete a provider by ID.",
		Tags:        []string{"providers"},
	}, func(ctx context.Context, input *ProviderByIDInput) (*struct{}, error) {
		provider, err := getProviderByID(ctx, registry, extensions, input.ProviderID, input.Platform)
		if err != nil {
			return nil, err
		}
		platform := strings.ToLower(strings.TrimSpace(provider.Platform))
		adapter, ok := extensions.ResolveProviderAdapter(platform)
		if !ok {
			return nil, unsupportedProviderPlatformError(platform)
		}
		err = adapter.DeleteProvider(ctx, input.ProviderID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Provider not found")
			}
			return nil, huma.Error500InternalServerError("Failed to delete provider", err)
		}
		return &struct{}{}, nil
	})
}

package providers

import (
	"context"
	"errors"
	"net/http"

	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

type ProviderListInput struct {
	Platform string `query:"platform" json:"platform,omitempty" doc:"Filter providers by platform type"`
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

func providerListHTTPError(err error) error {
	switch {
	case providersvc.IsUnsupportedPlatformError(err):
		return huma.Error400BadRequest(err.Error())
	default:
		return huma.Error500InternalServerError("Failed to list providers", err)
	}
}

func providerReadHTTPError(action string, err error) error {
	switch {
	case providersvc.IsUnsupportedPlatformError(err):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, database.ErrInvalidInput):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, database.ErrNotFound):
		return huma.Error404NotFound("Provider not found")
	default:
		return huma.Error500InternalServerError(action, err)
	}
}

func providerWriteHTTPError(action string, err error) error {
	switch {
	case providersvc.IsUnsupportedPlatformError(err):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, database.ErrInvalidInput):
		return huma.Error400BadRequest("Invalid provider input")
	case errors.Is(err, database.ErrAlreadyExists):
		return huma.Error409Conflict("Provider already exists")
	case errors.Is(err, database.ErrNotFound):
		return huma.Error404NotFound("Provider not found")
	default:
		return huma.Error500InternalServerError(action, err)
	}
}

func RegisterProvidersEndpoints(api huma.API, basePath string, providerSvc providersvc.Registry) {
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
		providers, err := providerSvc.ListProviders(ctx, input.Platform)
		if err != nil {
			return nil, providerListHTTPError(err)
		}
		for _, p := range providers {
			if p == nil {
				continue
			}
			resp.Body.Providers = append(resp.Body.Providers, *p)
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
		provider, err := providerSvc.RegisterProvider(ctx, &input.Body)
		if err != nil {
			return nil, providerWriteHTTPError("Failed to create provider", err)
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
		provider, err := providerSvc.ResolveProvider(ctx, input.ProviderID, input.Platform)
		if err != nil {
			return nil, providerReadHTTPError("Failed to get provider", err)
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
		updated, err := providerSvc.UpdateProvider(ctx, input.ProviderID, input.Platform, &input.Body)
		if err != nil {
			return nil, providerWriteHTTPError("Failed to update provider", err)
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
		err := providerSvc.DeleteProvider(ctx, input.ProviderID, input.Platform)
		if err != nil {
			return nil, providerWriteHTTPError("Failed to delete provider", err)
		}
		return &struct{}{}, nil
	})
}

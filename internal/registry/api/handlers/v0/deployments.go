package v0

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

type DeploymentRequest = apitypes.DeploymentRequest

// DeploymentResponse represents a deployment
type DeploymentResponse struct {
	Body models.Deployment
}

type DeploymentLogsResponse = apitypes.DeploymentLogsResponse

// DeploymentsListResponse represents a list of deployments
type DeploymentsListResponse struct {
	Body apitypes.DeploymentsListResponse
}

// DeploymentByIDInput represents path parameters for ID-based deployment operations.
type DeploymentByIDInput struct {
	ID string `path:"id" json:"id" doc:"Deployment ID" example:"6b7ce4ab-ec3d-4789-95f4-8be5fac2e6be"`
}

// DeploymentsListInput represents query parameters for listing deployments
type DeploymentsListInput struct {
	Platform     string `query:"platform" json:"platform,omitempty" doc:"Filter by provider platform type (matches registered provider platforms)" example:"local"`
	ProviderID   string `query:"providerId" json:"providerId,omitempty" doc:"Filter by provider instance ID"`
	ResourceType string `query:"resourceType" json:"resourceType,omitempty" doc:"Filter by resource type (mcp, agent)" example:"mcp" enum:"mcp,agent"`
	Status       string `query:"status" json:"status,omitempty" doc:"Filter by deployment status"`
	Origin       string `query:"origin" json:"origin,omitempty" doc:"Filter by deployment origin (managed, discovered)" enum:"managed,discovered"`
	ResourceName string `query:"resourceName" json:"resourceName,omitempty" doc:"Case-insensitive substring filter on resource name"`
}

func normalizePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func createDeploymentHTTPError(err error) error {
	switch {
	case service.IsUnsupportedDeploymentPlatformError(err):
		return huma.Error400BadRequest("Unsupported provider or platform for deployment")
	case errors.Is(err, database.ErrInvalidInput):
		return huma.Error400BadRequest(err.Error())
	case errors.Is(err, database.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, auth.ErrUnauthenticated):
		return huma.Error401Unauthorized("Authentication required")
	case errors.Is(err, auth.ErrForbidden):
		return huma.Error403Forbidden("Forbidden")
	case errors.Is(err, database.ErrAlreadyExists):
		return huma.Error409Conflict("Deployment with this ID already exists")
	case err.Error() == "agent deployment is not yet implemented":
		return huma.Error501NotImplemented("Agent deployment is not yet supported")
	default:
		return huma.Error500InternalServerError("Failed to deploy resource", err)
	}
}

func removeDeploymentHTTPError(err error) error {
	switch {
	case service.IsUnsupportedDeploymentPlatformError(err):
		return huma.Error400BadRequest("Unsupported provider or platform for deployment")
	case errors.Is(err, database.ErrInvalidInput):
		return huma.Error400BadRequest("Invalid deployment removal request")
	case errors.Is(err, database.ErrNotFound):
		return huma.Error404NotFound("Deployment not found")
	case errors.Is(err, auth.ErrUnauthenticated):
		return huma.Error401Unauthorized("Authentication required")
	case errors.Is(err, auth.ErrForbidden):
		return huma.Error403Forbidden("Forbidden")
	default:
		return huma.Error500InternalServerError("Failed to remove deployment", err)
	}
}

// RegisterDeploymentsEndpoints registers all deployment-related endpoints
func RegisterDeploymentsEndpoints(api huma.API, basePath string, registry service.RegistryService, extensions PlatformExtensions) {
	// List all deployments
	huma.Register(api, huma.Operation{
		OperationID: "list-deployments",
		Method:      http.MethodGet,
		Path:        basePath + "/deployments",
		Summary:     "List deployed resources",
		Description: "Retrieve all deployed resources (MCP servers, agents) with their configurations. Optionally filter by resource type.",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentsListInput) (*DeploymentsListResponse, error) {
		filter := &models.DeploymentFilter{}
		if input.Platform != "" {
			p := normalizePlatform(input.Platform)
			filter.Platform = &p
		}
		if input.ProviderID != "" {
			id := input.ProviderID
			filter.ProviderID = &id
		}
		if input.ResourceType != "" {
			t := input.ResourceType
			filter.ResourceType = &t
		}
		if input.Status != "" {
			s := input.Status
			filter.Status = &s
		}
		if input.Origin != "" {
			o := input.Origin
			filter.Origin = &o
		}
		if input.ResourceName != "" {
			n := input.ResourceName
			filter.ResourceName = &n
		}

		deployments, err := registry.GetDeployments(ctx, filter)
		if err != nil {
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployments", err)
		}

		resp := &DeploymentsListResponse{}
		resp.Body.Deployments = make([]models.Deployment, 0, len(deployments))
		for _, d := range deployments {
			resp.Body.Deployments = append(resp.Body.Deployments, *d)
		}

		return resp, nil
	})

	// Get a specific deployment
	huma.Register(api, huma.Operation{
		OperationID: "get-deployment",
		Method:      http.MethodGet,
		Path:        basePath + "/deployments/{id}",
		Summary:     "Get deployment details",
		Description: "Retrieve details for a specific deployment by ID",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentByIDInput) (*DeploymentResponse, error) {
		deployment, err := registry.GetDeploymentByID(ctx, input.ID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		return &DeploymentResponse{Body: *deployment}, nil
	})

	// Deploy a server
	huma.Register(api, huma.Operation{
		OperationID: "deploy-server",
		Method:      http.MethodPost,
		Path:        basePath + "/deployments",
		Summary:     "Deploy a resource",
		Description: "Deploy a resource (MCP server or agent) with deployment env vars (`env`) and optional provider-specific settings (`providerConfig`). Defaults to MCP server if resourceType is not specified.",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *struct {
		Body DeploymentRequest
	}) (*DeploymentResponse, error) {
		// Default to MCP server if resource type not specified
		resourceType := input.Body.ResourceType
		if resourceType == "" {
			resourceType = "mcp"
		}

		// Validate resource type
		if resourceType != "mcp" && resourceType != "agent" {
			return nil, huma.Error400BadRequest("Invalid resource type. Must be 'mcp' or 'agent'")
		}

		providerID := strings.TrimSpace(input.Body.ProviderID)
		if providerID == "" {
			return nil, huma.Error400BadRequest("providerId is required")
		}
		_, err := getProviderByID(ctx, registry, extensions, providerID, "")
		if err != nil {
			return nil, err
		}

		deploymentReq := &models.Deployment{
			ServerName:     input.Body.ServerName,
			Version:        input.Body.Version,
			ProviderID:     providerID,
			ResourceType:   resourceType,
			Origin:         "managed",
			Env:            input.Body.Env,
			ProviderConfig: input.Body.ProviderConfig,
			PreferRemote:   input.Body.PreferRemote,
		}

		deployment, err := registry.CreateDeployment(ctx, deploymentReq)
		if err != nil {
			return nil, createDeploymentHTTPError(err)
		}

		return &DeploymentResponse{Body: *deployment}, nil
	})

	// Remove a deployment
	huma.Register(api, huma.Operation{
		OperationID: "remove-deployment",
		Method:      http.MethodDelete,
		Path:        basePath + "/deployments/{id}",
		Summary:     "Remove a deployed resource",
		Description: "Remove a deployment by ID",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentByIDInput) (*struct{}, error) {
		deployment, err := registry.GetDeploymentByID(ctx, input.ID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		// Guard discovered deployments before provider/adapter resolution.
		if deployment.Origin == "discovered" {
			return nil, huma.Error409Conflict("Discovered deployments cannot be deleted directly")
		}

		err = registry.UndeployDeployment(ctx, deployment)
		if err != nil {
			return nil, removeDeploymentHTTPError(err)
		}

		return &struct{}{}, nil
	})

	// Get deployment logs (async providers)
	huma.Register(api, huma.Operation{
		OperationID: "get-deployment-logs",
		Method:      http.MethodGet,
		Path:        basePath + "/deployments/{id}/logs",
		Summary:     "Get deployment logs",
		Description: "Get logs for async deployments when supported by the provider",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentByIDInput) (*DeploymentLogsResponse, error) {
		deployment, err := registry.GetDeploymentByID(ctx, input.ID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		logs, err := registry.GetDeploymentLogs(ctx, deployment)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid deployment logs request")
			}
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment logs not found")
			}
			if errors.Is(err, utils.ErrDeploymentNotSupported) {
				return nil, huma.Error501NotImplemented("Deployment logs are not supported for this provider")
			}
			return nil, huma.Error500InternalServerError("Failed to fetch deployment logs", err)
		}
		return &DeploymentLogsResponse{
			DeploymentID: deployment.ID,
			Logs:         logs,
		}, nil
	})

	// Cancel in-progress deployment (async providers)
	huma.Register(api, huma.Operation{
		OperationID: "cancel-deployment",
		Method:      http.MethodPost,
		Path:        basePath + "/deployments/{id}/cancel",
		Summary:     "Cancel deployment",
		Description: "Cancel a deployment when supported by the provider",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentByIDInput) (*struct{}, error) {
		deployment, err := registry.GetDeploymentByID(ctx, input.ID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		if err := registry.CancelDeployment(ctx, deployment); err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid deployment cancel request")
			}
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment job not found")
			}
			if errors.Is(err, utils.ErrDeploymentNotSupported) {
				return nil, huma.Error501NotImplemented("Deployment cancel is not supported for this provider")
			}
			return nil, huma.Error500InternalServerError("Failed to cancel deployment", err)
		}
		return &struct{}{}, nil
	})
}

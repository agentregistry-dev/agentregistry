package v0

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

const LocalProviderID = "local"

// DeploymentRequest represents the input for deploying a server
type DeploymentRequest struct {
	ServerName     string            `json:"serverName" doc:"Server name to deploy" example:"io.github.user/weather"`
	Version        string            `json:"version" doc:"Version to deploy (use 'latest' for latest version)" default:"latest" example:"1.0.0"`
	Env            map[string]string `json:"env,omitempty" doc:"Deployment environment variables."`
	ProviderConfig map[string]any    `json:"providerConfig,omitempty" doc:"Optional provider-specific deployment settings (not env vars)."`
	PreferRemote   bool              `json:"preferRemote,omitempty" doc:"Prefer remote deployment over local" default:"false"`
	ResourceType   string            `json:"resourceType,omitempty" doc:"Type of resource to deploy (mcp, agent)" default:"mcp" example:"mcp" enum:"mcp,agent"`
	ProviderID     string            `json:"providerId,omitempty" doc:"Concrete provider instance ID. Defaults to local singleton when omitted."`
}

// DeploymentResponse represents a deployment
type DeploymentResponse struct {
	Body models.Deployment
}

type DeploymentLogsResponse struct {
	DeploymentID string   `json:"deploymentId"`
	Logs         []string `json:"logs"`
}

// DeploymentsListResponse represents a list of deployments
type DeploymentsListResponse struct {
	Body struct {
		Deployments []models.Deployment `json:"deployments" doc:"List of deployed servers"`
	}
}

// DeploymentByIDInput represents path parameters for ID-based deployment operations.
type DeploymentByIDInput struct {
	ID string `path:"id" json:"id" doc:"Deployment ID" example:"6b7ce4ab-ec3d-4789-95f4-8be5fac2e6be"`
}

// DeploymentsListInput represents query parameters for listing deployments
type DeploymentsListInput struct {
	Platform     string `query:"platform" json:"platform,omitempty" doc:"Filter by provider platform type (for OSS: local or kubernetes)" example:"local"`
	ProviderID   string `query:"providerId" json:"providerId,omitempty" doc:"Filter by provider instance ID"`
	ResourceType string `query:"resourceType" json:"resourceType,omitempty" doc:"Filter by resource type (mcp, agent)" example:"mcp" enum:"mcp,agent"`
	Status       string `query:"status" json:"status,omitempty" doc:"Filter by deployment status"`
	Origin       string `query:"origin" json:"origin,omitempty" doc:"Filter by deployment origin (managed, discovered)" enum:"managed,discovered"`
	ResourceName string `query:"resourceName" json:"resourceName,omitempty" doc:"Case-insensitive substring filter on resource name"`
}

func normalizePlatform(platform string) string {
	return strings.ToLower(strings.TrimSpace(platform))
}

func deploymentPlatform(ctx context.Context, registry service.RegistryService, deployment *models.Deployment) string {
	if deployment == nil {
		return ""
	}
	if strings.TrimSpace(deployment.ProviderID) == "" {
		return ""
	}
	provider, err := registry.GetProviderByID(ctx, deployment.ProviderID)
	if err != nil || provider == nil {
		return ""
	}
	return normalizePlatform(provider.Platform)
}

func unsupportedDeploymentPlatformError(platform string) error {
	p := strings.TrimSpace(platform)
	if p == "" {
		p = "unknown"
	}
	return huma.Error400BadRequest("Deployment platform is not supported: " + p)
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
			if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Not found")
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
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
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
			providerID = LocalProviderID
		}
		provider, err := getProviderByID(ctx, registry, extensions, providerID, "")
		if err != nil {
			return nil, err
		}
		platform := normalizePlatform(provider.Platform)

		var deployment *models.Deployment

		// Use adapter dispatch when present (built-ins + enterprise extensions).
		adapter, ok := extensions.ResolveDeploymentAdapter(platform)
		if !ok {
			return nil, unsupportedDeploymentPlatformError(platform)
		}
		deploymentReq := &models.Deployment{
			ServerName:     input.Body.ServerName,
			Version:        input.Body.Version,
			Env:            input.Body.Env,
			ProviderConfig: input.Body.ProviderConfig,
			PreferRemote:   input.Body.PreferRemote,
			ResourceType:   resourceType,
			ProviderID:     providerID,
		}
		deployment, err = adapter.Deploy(ctx, deploymentReq)

		if err != nil {
			if errors.Is(err, errProviderConfigNotSupported) {
				return nil, huma.Error400BadRequest("providerConfig is not supported for this provider platform")
			}
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid deployment request")
			}
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Resource not found in registry")
			}
			if errors.Is(err, database.ErrAlreadyExists) {
				return nil, huma.Error409Conflict("Resource is already deployed")
			}
			// Check for "not yet implemented" error
			if err.Error() == "agent deployment is not yet implemented" {
				return nil, huma.Error501NotImplemented("Agent deployment is not yet supported")
			}
			return nil, huma.Error500InternalServerError("Failed to deploy resource", err)
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
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		// Guard discovered deployments before provider/adapter resolution.
		if deployment.Origin == "discovered" {
			return nil, huma.Error409Conflict("Discovered deployments cannot be deleted directly")
		}

		platform := deploymentPlatform(ctx, registry, deployment)
		adapter, ok := extensions.ResolveDeploymentAdapter(platform)
		if !ok {
			return nil, unsupportedDeploymentPlatformError(platform)
		}
		err = adapter.Undeploy(ctx, deployment)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error409Conflict("Discovered deployments cannot be deleted directly")
			}
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to remove deployment", err)
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
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		platform := deploymentPlatform(ctx, registry, deployment)
		adapter, ok := extensions.ResolveDeploymentAdapter(platform)
		if !ok {
			return nil, unsupportedDeploymentPlatformError(platform)
		}
		logs, err := adapter.GetLogs(ctx, deployment)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid deployment logs request")
			}
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment logs not found")
			}
			if errors.Is(err, errDeploymentNotSupported) {
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
		Description: "Cancel an in-progress deployment when supported by the provider",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentByIDInput) (*struct{}, error) {
		deployment, err := registry.GetDeploymentByID(ctx, input.ID)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to retrieve deployment", err)
		}

		if deployment.Status != "deploying" {
			return nil, huma.Error400BadRequest("Deployment is not in progress")
		}

		platform := deploymentPlatform(ctx, registry, deployment)
		adapter, ok := extensions.ResolveDeploymentAdapter(platform)
		if !ok {
			return nil, unsupportedDeploymentPlatformError(platform)
		}
		if err := adapter.Cancel(ctx, deployment); err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest("Invalid deployment cancel request")
			}
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Deployment job not found")
			}
			if errors.Is(err, errDeploymentNotSupported) {
				return nil, huma.Error501NotImplemented("Deployment cancel is not supported for this provider")
			}
			return nil, huma.Error500InternalServerError("Failed to cancel deployment", err)
		}
		return &struct{}{}, nil
	})
}

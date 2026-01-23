package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

// DeploymentRequest represents the input for deploying a server
type DeploymentRequest struct {
	ServerName   string            `json:"serverName" doc:"Server name to deploy" example:"io.github.user/weather"`
	Version      string            `json:"version" doc:"Version to deploy (use 'latest' for latest version)" default:"latest" example:"1.0.0"`
	Config       map[string]string `json:"config,omitempty" doc:"Configuration key-value pairs (env vars, args, headers)"`
	PreferRemote bool              `json:"preferRemote,omitempty" doc:"Prefer remote deployment over local" default:"false"`
	ResourceType string            `json:"resourceType,omitempty" doc:"Type of resource to deploy (mcp, agent)" default:"mcp" example:"mcp" enum:"mcp,agent"`
	Runtime      string            `json:"runtime,omitempty" doc:"Runtime target (local, kubernetes)" default:"local" example:"local" enum:"local,kubernetes"`
}

// DeploymentConfigUpdate represents the input for updating deployment configuration
type DeploymentConfigUpdate struct {
	Config map[string]string `json:"config" doc:"Configuration key-value pairs to set"`
}

// DeploymentResponse represents a deployment
type DeploymentResponse struct {
	Body models.Deployment
}

// DeploymentsListResponse represents a list of deployments
type DeploymentsListResponse struct {
	Body struct {
		Deployments []models.Deployment `json:"deployments" doc:"List of deployed servers"`
	}
}

// DeploymentInput represents path parameters for deployment operations
type DeploymentInput struct {
	ServerName   string `path:"serverName" json:"serverName" doc:"URL-encoded server name" example:"io.github.user%2Fweather"`
	Version      string `path:"version" json:"version" doc:"Version of the deployment to get" example:"1.0.0"`
	ResourceType string `query:"resourceType" json:"resourceType" doc:"Resource type (mcp, agent)" example:"mcp" enum:"mcp,agent"`
}

// DeploymentsListInput represents query parameters for listing deployments
type DeploymentsListInput struct {
	ResourceType string `query:"resourceType" json:"resourceType,omitempty" doc:"Filter by resource type (mcp, agent)" example:"mcp" enum:"mcp,agent"`
	Runtime      string `query:"runtime" json:"runtime,omitempty" doc:"Filter by runtime (local, kubernetes)" example:"local" enum:"local,kubernetes"`
}

// RegisterDeploymentsEndpoints registers all deployment-related endpoints
func RegisterDeploymentsEndpoints(api huma.API, basePath string, registry service.RegistryService) {
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
		if input.ResourceType != "" {
			t := input.ResourceType
			filter.ResourceType = &t
		}
		if input.Runtime != "" {
			r := input.Runtime
			filter.Runtime = &r
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
		Path:        basePath + "/deployments/{serverName}/versions/{version}",
		Summary:     "Get deployment details",
		Description: "Retrieve details for a specific deployed resource (MCP server or agent)",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *struct {
		DeploymentInput
	}) (*DeploymentResponse, error) {
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		deployment, err := registry.GetDeploymentByNameAndVersion(ctx, serverName, version, input.ResourceType)
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
		Description: "Deploy a resource (MCP server or agent) with optional configuration. Defaults to MCP server if resourceType is not specified.",
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

		runtimeTarget := input.Body.Runtime
		if runtimeTarget == "" {
			runtimeTarget = "local"
		}
		if err := runtime.ValidateRuntime(runtimeTarget); err != nil {
			return nil, huma.Error400BadRequest("Invalid runtime target", err)
		}

		var deployment *models.Deployment
		var err error

		// Route to appropriate service method based on resource type
		switch resourceType {
		case "mcp":
			deployment, err = registry.DeployServer(ctx, input.Body.ServerName, input.Body.Version, input.Body.Config, input.Body.PreferRemote, runtimeTarget)
		case "agent":
			deployment, err = registry.DeployAgent(ctx, input.Body.ServerName, input.Body.Version, input.Body.Config, input.Body.PreferRemote, runtimeTarget)
		}

		if err != nil {
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

	// Update deployment configuration
	huma.Register(api, huma.Operation{
		OperationID: "update-deployment-config",
		Method:      http.MethodPut,
		Path:        basePath + "/deployments/{serverName}/versions/{version}",
		Summary:     "Update deployment configuration",
		Description: "Update the configuration (env vars, args, headers) for a deployed resource (MCP server or agent)",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *struct {
		DeploymentInput
		Body DeploymentConfigUpdate
	}) (*DeploymentResponse, error) {
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		deployment, err := registry.UpdateDeploymentConfig(ctx, serverName, version, input.ResourceType, input.Body.Config)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to update deployment configuration", err)
		}

		return &DeploymentResponse{Body: *deployment}, nil
	})

	// Remove a deployment
	huma.Register(api, huma.Operation{
		OperationID: "remove-deployment",
		Method:      http.MethodDelete,
		Path:        basePath + "/deployments/{serverName}/versions/{version}",
		Summary:     "Remove a deployed resource",
		Description: "Remove a deployment from deployed state",
		Tags:        []string{"deployments"},
	}, func(ctx context.Context, input *DeploymentInput) (*struct{}, error) {
		switch input.ResourceType {
		case "mcp", "agent":
			// Valid resource types
		default:
			return nil, huma.Error400BadRequest("Invalid resource type. Must be 'mcp' or 'agent'. Got: " + input.ResourceType)
		}

		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		err = registry.RemoveDeployment(ctx, serverName, version, input.ResourceType)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Deployment not found")
			}
			return nil, huma.Error500InternalServerError("Failed to remove deployment", err)
		}

		return &struct{}{}, nil
	})
}

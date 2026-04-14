package servers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"

	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

// ServerDeploymentApplyInput represents path parameters for apply-server-deployment.
type ServerDeploymentApplyInput struct {
	ServerName string                    `path:"serverName" doc:"URL-encoded server name"`
	Version    string                    `path:"version" doc:"URL-encoded server version"`
	ProviderID string                    `path:"providerId" doc:"Deployment provider ID"`
	Body       ServerDeploymentApplyBody `body:""`
}

// ServerDeploymentApplyBody carries the mutable fields for a server deployment apply request.
type ServerDeploymentApplyBody struct {
	Env            map[string]string `json:"env,omitempty" doc:"Deployment environment variables."`
	ProviderConfig map[string]any    `json:"providerConfig,omitempty" doc:"Optional provider-specific deployment settings."`
	PreferRemote   bool              `json:"preferRemote,omitempty" doc:"Prefer remote deployment over local" default:"false"`
}

// ServerDeploymentApplyResponse wraps a single deployment.
type ServerDeploymentApplyResponse struct {
	Body models.Deployment
}

// RegisterServersDeploymentApplyEndpoint registers
// PUT /v0/servers/{serverName}/versions/{version}/deployments/{providerId}
func RegisterServersDeploymentApplyEndpoint(api huma.API, pathPrefix string, deploymentSvc deploymentsvc.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "apply-server-deployment",
		Method:      http.MethodPut,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}/deployments/{providerId}",
		Summary:     "Apply server deployment (idempotent)",
		Description: "Idempotently deploy an MCP server. Returns the existing deployment unchanged if already deployed; otherwise cleans up any stale record and launches a fresh deployment.",
		Tags:        []string{"deployments", "servers"},
	}, func(ctx context.Context, input *ServerDeploymentApplyInput) (*ServerDeploymentApplyResponse, error) {
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}
		providerID := strings.TrimSpace(input.ProviderID)
		if providerID == "" {
			return nil, huma.Error400BadRequest("providerId is required")
		}

		deployment, err := deploymentSvc.ApplyServerDeployment(ctx, serverName, version, providerID, input.Body.Env, input.Body.ProviderConfig)
		if err != nil {
			return nil, serverApplyDeploymentHTTPError(err)
		}
		return &ServerDeploymentApplyResponse{Body: *deployment}, nil
	})
}

func serverApplyDeploymentHTTPError(err error) error {
	switch {
	case deploymentsvc.IsUnsupportedDeploymentPlatformError(err):
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
	default:
		return huma.Error500InternalServerError("Failed to deploy resource", err)
	}
}

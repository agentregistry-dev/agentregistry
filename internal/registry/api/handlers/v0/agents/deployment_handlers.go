package agents

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

// AgentDeploymentApplyInput represents path parameters for apply-agent-deployment.
type AgentDeploymentApplyInput struct {
	AgentName  string              `path:"agentName" doc:"URL-encoded agent name"`
	Version    string              `path:"version" doc:"URL-encoded agent version"`
	ProviderID string              `path:"providerId" doc:"Deployment provider ID"`
	Body       DeploymentApplyBody `body:""`
}

// DeploymentApplyBody carries the mutable fields for a deployment apply request.
type DeploymentApplyBody struct {
	Env            map[string]string `json:"env,omitempty" doc:"Deployment environment variables."`
	ProviderConfig map[string]any    `json:"providerConfig,omitempty" doc:"Optional provider-specific deployment settings."`
	PreferRemote   bool              `json:"preferRemote,omitempty" doc:"Prefer remote deployment over local" default:"false"`
}

// DeploymentApplyResponse wraps a single deployment.
type DeploymentApplyResponse struct {
	Body models.Deployment
}

// RegisterAgentsDeploymentApplyEndpoint registers
// PUT /v0/agents/{agentName}/versions/{version}/deployments/{providerId}
func RegisterAgentsDeploymentApplyEndpoint(api huma.API, pathPrefix string, deploymentSvc deploymentsvc.Registry) {
	huma.Register(api, huma.Operation{
		OperationID: "apply-agent-deployment",
		Method:      http.MethodPut,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}/deployments/{providerId}",
		Summary:     "Apply agent deployment (idempotent)",
		Description: "Idempotently deploy an agent. Returns the existing deployment unchanged if already deployed; otherwise cleans up any stale record and launches a fresh deployment.",
		Tags:        []string{"deployments", "agents"},
	}, func(ctx context.Context, input *AgentDeploymentApplyInput) (*DeploymentApplyResponse, error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}
		providerID := strings.TrimSpace(input.ProviderID)
		if providerID == "" {
			return nil, huma.Error400BadRequest("providerId is required")
		}

		deployment, err := deploymentSvc.ApplyAgentDeployment(ctx, agentName, version, providerID, input.Body.Env, input.Body.ProviderConfig)
		if err != nil {
			return nil, applyDeploymentHTTPError(err)
		}
		return &DeploymentApplyResponse{Body: *deployment}, nil
	})
}

func applyDeploymentHTTPError(err error) error {
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

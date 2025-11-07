package v0

import (
	"context"
	"net/http"
	"strings"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// PublishAgentInput represents the input for publishing an agent
type PublishAgentInput struct {
	Body agentmodels.AgentJSON `body:""`
}

// RegisterAgentsPublishEndpoint registers the agents publish endpoint with a custom path prefix
func RegisterAgentsPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {

	huma.Register(api, huma.Operation{
		OperationID: "publish-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/publish",
		Summary:     "Publish Agentic agent",
		Description: "Publish a new Agentic agent to the registry or update an existing one",
		Tags:        []string{"publish"},
	}, func(ctx context.Context, input *PublishAgentInput) (*Response[agentmodels.AgentResponse], error) {

		if err := authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{Name: input.Body.Name, Type: "agent"}); err != nil {
			return nil, err
		}

		publishedAgent, err := registry.CreateAgent(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish agent", err)
		}

		return &Response[agentmodels.AgentResponse]{Body: *publishedAgent}, nil
	})
}

package v0

import (
	"context"
	"net/http"
	"strings"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// PublishAgentInput represents the input for publishing an agent
type PublishAgentInput struct {
	Authorization string                `header:"Authorization" doc:"Registry JWT token (obtained from /v0/auth/token/github)" required:"true"`
	Body          agentmodels.AgentJSON `body:""`
}

// RegisterAgentsPublishEndpoint registers the agents publish endpoint with a custom path prefix
func RegisterAgentsPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	huma.Register(api, huma.Operation{
		OperationID: "publish-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/publish",
		Summary:     "Publish Agentic agent",
		Description: "Publish a new Agentic agent to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *PublishAgentInput) (*Response[agentmodels.AgentResponse], error) {
		const bearerPrefix = "Bearer "
		authHeader := input.Authorization
		if len(authHeader) < len(bearerPrefix) || !strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
			return nil, huma.Error401Unauthorized("Invalid Authorization header format. Expected 'Bearer <token>'")
		}
		token := authHeader[len(bearerPrefix):]

		claims, err := jwtManager.ValidateToken(ctx, token)
		if err != nil {
			return nil, huma.Error401Unauthorized("Invalid or expired Registry JWT token", err)
		}

		if !jwtManager.HasPermission(input.Body.Name, auth.PermissionActionPublish, claims.Permissions) {
			return nil, huma.Error403Forbidden(buildPermissionErrorMessage(input.Body.Name, claims.Permissions))
		}

		publishedAgent, err := registry.CreateAgent(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish agent", err)
		}

		return &Response[agentmodels.AgentResponse]{Body: *publishedAgent}, nil
	})
}

package v0

import (
	"context"
	"net/http"
	"strings"

	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// PublishSkillInput represents the input for publishing a skill
type PublishSkillInput struct {
	Authorization string                `header:"Authorization" doc:"Registry JWT token (obtained from /v0/auth/token/github)" required:"true"`
	Body          skillmodels.SkillJSON `body:""`
}

// RegisterSkillsPublishEndpoint registers the skills publish endpoint with a custom path prefix
func RegisterSkillsPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, cfg *config.Config) {
	jwtManager := auth.NewJWTManager(cfg)

	huma.Register(api, huma.Operation{
		OperationID: "publish-skill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills/publish",
		Summary:     "Publish Agentic skill",
		Description: "Publish a new Agentic skill to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *PublishSkillInput) (*Response[skillmodels.SkillResponse], error) {
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

		publishedSkill, err := registry.CreateSkill(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish skill", err)
		}

		return &Response[skillmodels.SkillResponse]{Body: *publishedSkill}, nil
	})
}

package v0

import (
	"context"
	"net/http"
	"strings"

	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// PublishSkillInput represents the input for publishing a skill
type PublishSkillInput struct {
	Body skillmodels.SkillJSON `body:""`
}

// RegisterSkillsPublishEndpoint registers the skills publish endpoint with a custom path prefix
func RegisterSkillsPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {

	huma.Register(api, huma.Operation{
		OperationID: "publish-skill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills/publish",
		Summary:     "Publish Agentic skill",
		Description: "Publish a new Agentic skill to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *PublishSkillInput) (*Response[skillmodels.SkillResponse], error) {

		if err := authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{Name: input.Body.Name, Type: "skill"}); err != nil {
			return nil, err
		}

		publishedSkill, err := registry.CreateSkill(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish skill", err)
		}

		return &Response[skillmodels.SkillResponse]{Body: *publishedSkill}, nil
	})
}

package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	skillmodels "github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

// ListSkillsInput represents the input for listing skills
type ListSkillsInput struct {
	Cursor       string `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"skill-cursor-123"`
	Limit        int    `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" json:"updated_since,omitempty" doc:"Filter skills updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" json:"search,omitempty" doc:"Search skills by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// SkillDetailInput represents the input for getting skill details
type SkillDetailInput struct {
	SkillName string `path:"skillName" json:"skillName" doc:"URL-encoded skill name" example:"com.example%2Fmy-skill"`
}

// SkillVersionDetailInput represents the input for getting a specific version
type SkillVersionDetailInput struct {
	SkillName string `path:"skillName" json:"skillName" doc:"URL-encoded skill name" example:"com.example%2Fmy-skill"`
	Version   string `path:"version" json:"version" doc:"URL-encoded skill version" example:"1.0.0"`
}

// SkillVersionsInput represents the input for listing all versions of a skill
type SkillVersionsInput struct {
	SkillName string `path:"skillName" json:"skillName" doc:"URL-encoded skill name" example:"com.example%2Fmy-skill"`
}

// RegisterSkillsEndpoints registers all skill-related endpoints with a custom path prefix.
func RegisterSkillsEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	tags := []string{"skills"}
	if strings.Contains(pathPrefix, "admin") {
		tags = append(tags, "admin")
	}

	// List skills
	huma.Register(api, huma.Operation{
		OperationID: "list-skills" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/skills",
		Summary:     "List Agentic skills",
		Description: "Get a paginated list of Agentic skills from the registry",
		Tags:        tags,
	}, func(ctx context.Context, input *ListSkillsInput) (*types.Response[skillmodels.SkillListResponse], error) {
		// Note: Authz filtering for list operations is handled at the database layer.

		// Build filter
		filter := &database.SkillFilter{}

		if input.UpdatedSince != "" {
			if updatedTime, err := time.Parse(time.RFC3339, input.UpdatedSince); err == nil {
				filter.UpdatedSince = &updatedTime
			} else {
				return nil, huma.Error400BadRequest("Invalid updated_since format: expected RFC3339 timestamp (e.g., 2025-08-07T13:15:04.280Z)")
			}
		}
		if input.Search != "" {
			filter.SubstringName = &input.Search
		}
		if input.Version != "" {
			if input.Version == "latest" {
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				filter.Version = &input.Version
			}
		}

		skills, nextCursor, err := registry.ListSkills(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get skills list", err)
		}

		skillValues := make([]skillmodels.SkillResponse, len(skills))
		for i, s := range skills {
			skillValues[i] = *s
		}
		return &types.Response[skillmodels.SkillListResponse]{
			Body: skillmodels.SkillListResponse{
				Skills: skillValues,
				Metadata: skillmodels.SkillMetadata{
					NextCursor: nextCursor,
					Count:      len(skills),
				},
			},
		}, nil
	})

	// Get specific skill version (supports "latest")
	huma.Register(api, huma.Operation{
		OperationID: "get-skill-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/skills/{skillName}/versions/{version}",
		Summary:     "Get specific Agentic skill version",
		Description: "Get detailed information about a specific version of an Agentic skill. Use the special version 'latest' to get the latest version.",
		Tags:        tags,
	}, func(ctx context.Context, input *SkillVersionDetailInput) (*types.Response[skillmodels.SkillResponse], error) {
		skillName, err := url.PathUnescape(input.SkillName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid skill name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		var skillResp *skillmodels.SkillResponse
		if version == "latest" {
			skillResp, err = registry.GetSkillByName(ctx, skillName)
		} else {
			skillResp, err = registry.GetSkillByNameAndVersion(ctx, skillName, version)
		}
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get skill details", err)
		}
		return &types.Response[skillmodels.SkillResponse]{Body: *skillResp}, nil
	})

	// Get all versions for a skill
	huma.Register(api, huma.Operation{
		OperationID: "get-skill-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/skills/{skillName}/versions",
		Summary:     "Get all versions of an Agentic skill",
		Description: "Get all available versions for a specific Agentic skill",
		Tags:        tags,
	}, func(ctx context.Context, input *SkillVersionsInput) (*types.Response[skillmodels.SkillListResponse], error) {
		skillName, err := url.PathUnescape(input.SkillName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid skill name encoding", err)
		}

		// Get all versions of the skill
		skills, err := registry.GetAllVersionsBySkillName(ctx, skillName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get skill versions", err)
		}

		skillValues := make([]skillmodels.SkillResponse, len(skills))
		for i, s := range skills {
			skillValues[i] = *s
		}
		return &types.Response[skillmodels.SkillListResponse]{
			Body: skillmodels.SkillListResponse{
				Skills:   skillValues,
				Metadata: skillmodels.SkillMetadata{Count: len(skills)},
			},
		}, nil
	})
}

// CreateSkillInput represents the input for creating/updating a skill
type CreateSkillInput struct {
	Body skillmodels.SkillJSON `body:""`
}

// createSkillHandler is the shared handler logic for creating skills
func createSkillHandler(ctx context.Context, input *CreateSkillInput, registry service.RegistryService) (*types.Response[skillmodels.SkillResponse], error) {
	// Create/update the skill (published defaults to false in the service layer)
	createdSkill, err := registry.CreateSkill(ctx, &input.Body)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
			return nil, huma.Error404NotFound("Not found")
		}
		return nil, huma.Error400BadRequest("Failed to create skill", err)
	}

	return &types.Response[skillmodels.SkillResponse]{Body: *createdSkill}, nil
}

// RegisterSkillsCreateEndpoint registers POST /skills (create or update; immediately visible).
func RegisterSkillsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService) {
	huma.Register(api, huma.Operation{
		OperationID: "create-skill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills",
		Summary:     "Create or update skill",
		Description: "Create a new Agentic skill in the registry or update an existing one. Resources are immediately visible after creation.",
		Tags:        []string{"skills"},
	}, func(ctx context.Context, input *CreateSkillInput) (*types.Response[skillmodels.SkillResponse], error) {
		return createSkillHandler(ctx, input, registry)
	})
}

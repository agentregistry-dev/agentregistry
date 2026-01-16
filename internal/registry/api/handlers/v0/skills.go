package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	skillmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
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

// RegisterSkillsEndpoints registers all skill-related endpoints with a custom path prefix
// isAdmin: if true, shows all resources; if false, only shows published resources
func RegisterSkillsEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, isAdmin bool) {
	// Determine the tags based on whether this is admin or public
	tags := []string{"skills"}
	if isAdmin {
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
	}, func(ctx context.Context, input *ListSkillsInput) (*Response[skillmodels.SkillListResponse], error) {
		// Build filter
		filter := &database.SkillFilter{}

		// For public endpoints, only show published resources
		if !isAdmin {
			published := true
			filter.Published = &published
		}

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
			return nil, huma.Error500InternalServerError("Failed to get skills list", err)
		}

		skillValues := make([]skillmodels.SkillResponse, len(skills))
		for i, s := range skills {
			skillValues[i] = *s
		}
		return &Response[skillmodels.SkillListResponse]{
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
	}, func(ctx context.Context, input *SkillVersionDetailInput) (*Response[skillmodels.SkillResponse], error) {
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
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get skill details", err)
		}
		return &Response[skillmodels.SkillResponse]{Body: *skillResp}, nil
	})

	// Get all versions for a skill
	huma.Register(api, huma.Operation{
		OperationID: "get-skill-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/skills/{skillName}/versions",
		Summary:     "Get all versions of an Agentic skill",
		Description: "Get all available versions for a specific Agentic skill",
		Tags:        tags,
	}, func(ctx context.Context, input *SkillVersionsInput) (*Response[skillmodels.SkillListResponse], error) {
		skillName, err := url.PathUnescape(input.SkillName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid skill name encoding", err)
		}

		skills, err := registry.GetAllVersionsBySkillName(ctx, skillName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get skill versions", err)
		}

		skillValues := make([]skillmodels.SkillResponse, len(skills))
		for i, s := range skills {
			skillValues[i] = *s
		}
		return &Response[skillmodels.SkillListResponse]{
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
func createSkillHandler(ctx context.Context, input *CreateSkillInput, registry service.RegistryService) (*Response[skillmodels.SkillResponse], error) {
	// Create/update the skill (published defaults to false in the service layer)
	createdSkill, err := registry.CreateSkill(ctx, &input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to create skill", err)
	}

	return &Response[skillmodels.SkillResponse]{Body: *createdSkill}, nil
}

// RegisterSkillsCreateEndpoint registers the public skills create/update endpoint at /skills/publish
// This endpoint creates or updates a skill in the registry (published defaults to false)
func RegisterSkillsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "create-skill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills/publish",
		Summary:     "Create/update Agentic skill",
		Description: "Create a new Agentic skill in the registry or update an existing one. By default, skills are created as unpublished (published=false).",
		Tags:        []string{"skills", "publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *CreateSkillInput) (*Response[skillmodels.SkillResponse], error) {
		return createSkillHandler(ctx, input, registry)
	})
}

// RegisterAdminSkillsCreateEndpoint registers the admin skills create/update endpoint at /skills
// This endpoint creates or updates a skill in the registry (published defaults to false)
func RegisterAdminSkillsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-create-skill" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills",
		Summary:     "Create/update Agentic skill (Admin)",
		Description: "Create a new Agentic skill in the registry or update an existing one. By default, skills are created as unpublished (published=false).",
		Tags:        []string{"skills", "admin"},
	}, func(ctx context.Context, input *CreateSkillInput) (*Response[skillmodels.SkillResponse], error) {
		// Create/update the skill (published defaults to false in the service layer)
		createdSkill, err := registry.CreateSkill(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to create skill", err)
		}

		return &Response[skillmodels.SkillResponse]{Body: *createdSkill}, nil
	})
}

// RegisterSkillsPublishStatusEndpoints registers the publish/unpublish status endpoints for skills
// These endpoints change the published status of existing skills
func RegisterSkillsPublishStatusEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	// Publish skill endpoint - marks an existing skill as published
	huma.Register(api, huma.Operation{
		OperationID: "publish-skill-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills/{skillName}/versions/{version}/publish",
		Summary:     "Publish an existing skill",
		Description: "Mark an existing skill version as published, making it visible in public listings. This acts on a skill that was already created.",
		Tags:        []string{"skills", "admin"},
	}, func(ctx context.Context, input *SkillVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the skill name and version
		skillName, err := url.PathUnescape(input.SkillName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid skill name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to publish the skill
		if err := registry.PublishSkill(ctx, skillName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to publish skill", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Skill published successfully",
			},
		}, nil
	})

	// Unpublish skill endpoint - marks an existing skill as unpublished
	huma.Register(api, huma.Operation{
		OperationID: "unpublish-skill-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/skills/{skillName}/versions/{version}/unpublish",
		Summary:     "Unpublish an existing skill",
		Description: "Mark an existing skill version as unpublished, hiding it from public listings. This acts on a skill that was already created.",
		Tags:        []string{"skills", "admin"},
	}, func(ctx context.Context, input *SkillVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the skill name and version
		skillName, err := url.PathUnescape(input.SkillName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid skill name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to unpublish the skill
		if err := registry.UnpublishSkill(ctx, skillName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Skill not found")
			}
			return nil, huma.Error500InternalServerError("Failed to unpublish skill", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Skill unpublished successfully",
			},
		}, nil
	})
}

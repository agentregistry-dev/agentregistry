package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	promptmodels "github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

// ListPromptsInput represents the input for listing prompts
type ListPromptsInput struct {
	Cursor       string `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"prompt-cursor-123"`
	Limit        int    `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" json:"updated_since,omitempty" doc:"Filter prompts updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" json:"search,omitempty" doc:"Search prompts by name (substring match)" required:"false" example:"code-review"`
	Version      string `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// PromptDetailInput represents the input for getting prompt details
type PromptDetailInput struct {
	PromptName string `path:"promptName" json:"promptName" doc:"URL-encoded prompt name" example:"com.example%2Fmy-prompt"`
}

// PromptVersionDetailInput represents the input for getting a specific version
type PromptVersionDetailInput struct {
	PromptName string `path:"promptName" json:"promptName" doc:"URL-encoded prompt name" example:"com.example%2Fmy-prompt"`
	Version    string `path:"version" json:"version" doc:"URL-encoded prompt version" example:"1.0.0"`
}

// PromptVersionsInput represents the input for listing all versions of a prompt
type PromptVersionsInput struct {
	PromptName string `path:"promptName" json:"promptName" doc:"URL-encoded prompt name" example:"com.example%2Fmy-prompt"`
}

// RegisterPromptsEndpoints registers all prompt-related endpoints with a custom path prefix.
func RegisterPromptsEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	tags := []string{"prompts"}
	if strings.Contains(pathPrefix, "admin") {
		tags = append(tags, "admin")
	}

	// List prompts
	huma.Register(api, huma.Operation{
		OperationID: "list-prompts" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/prompts",
		Summary:     "List prompts",
		Description: "Get a paginated list of prompts from the registry",
		Tags:        tags,
	}, func(ctx context.Context, input *ListPromptsInput) (*types.Response[promptmodels.PromptListResponse], error) {
		// Build filter
		filter := &database.PromptFilter{}

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

		prompts, nextCursor, err := registry.ListPrompts(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			if errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get prompts list", err)
		}

		promptValues := make([]promptmodels.PromptResponse, len(prompts))
		for i, p := range prompts {
			promptValues[i] = *p
		}
		return &types.Response[promptmodels.PromptListResponse]{
			Body: promptmodels.PromptListResponse{
				Prompts: promptValues,
				Metadata: promptmodels.PromptMetadata{
					NextCursor: nextCursor,
					Count:      len(prompts),
				},
			},
		}, nil
	})

	// Get specific prompt version (supports "latest")
	huma.Register(api, huma.Operation{
		OperationID: "get-prompt-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/prompts/{promptName}/versions/{version}",
		Summary:     "Get specific prompt version",
		Description: "Get detailed information about a specific version of a prompt. Use the special version 'latest' to get the latest version.",
		Tags:        tags,
	}, func(ctx context.Context, input *PromptVersionDetailInput) (*types.Response[promptmodels.PromptResponse], error) {
		promptName, err := url.PathUnescape(input.PromptName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid prompt name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		var promptResp *promptmodels.PromptResponse
		if version == "latest" {
			promptResp, err = registry.GetPromptByName(ctx, promptName)
		} else {
			promptResp, err = registry.GetPromptByNameAndVersion(ctx, promptName, version)
		}
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Prompt not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get prompt details", err)
		}
		return &types.Response[promptmodels.PromptResponse]{Body: *promptResp}, nil
	})

	// Delete a specific prompt version
	huma.Register(api, huma.Operation{
		OperationID: "delete-prompt-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodDelete,
		Path:        pathPrefix + "/prompts/{promptName}/versions/{version}",
		Summary:     "Delete a prompt version",
		Description: "Permanently delete a specific prompt version from the registry.",
		Tags:        tags,
	}, func(ctx context.Context, input *PromptVersionDetailInput) (*types.Response[types.EmptyResponse], error) {
		promptName, err := url.PathUnescape(input.PromptName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid prompt name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		if err := registry.DeletePrompt(ctx, promptName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Prompt not found")
			}
			return nil, huma.Error500InternalServerError("Failed to delete prompt", err)
		}

		return &types.Response[types.EmptyResponse]{
			Body: types.EmptyResponse{Message: "Prompt deleted successfully"},
		}, nil
	})

	// Get all versions for a prompt
	huma.Register(api, huma.Operation{
		OperationID: "get-prompt-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/prompts/{promptName}/versions",
		Summary:     "Get all versions of a prompt",
		Description: "Get all available versions for a specific prompt",
		Tags:        tags,
	}, func(ctx context.Context, input *PromptVersionsInput) (*types.Response[promptmodels.PromptListResponse], error) {
		promptName, err := url.PathUnescape(input.PromptName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid prompt name encoding", err)
		}

		prompts, err := registry.GetAllVersionsByPromptName(ctx, promptName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error404NotFound("Prompt not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get prompt versions", err)
		}

		promptValues := make([]promptmodels.PromptResponse, len(prompts))
		for i, p := range prompts {
			promptValues[i] = *p
		}
		return &types.Response[promptmodels.PromptListResponse]{
			Body: promptmodels.PromptListResponse{
				Prompts:  promptValues,
				Metadata: promptmodels.PromptMetadata{Count: len(prompts)},
			},
		}, nil
	})
}

// CreatePromptInput represents the input for creating/updating a prompt
type CreatePromptInput struct {
	Body promptmodels.PromptJSON `body:""`
}

// createPromptHandler is the shared handler logic for creating prompts
func createPromptHandler(ctx context.Context, input *CreatePromptInput, registry service.RegistryService) (*types.Response[promptmodels.PromptResponse], error) {
	createdPrompt, err := registry.CreatePrompt(ctx, &input.Body)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) || errors.Is(err, auth.ErrForbidden) || errors.Is(err, auth.ErrUnauthenticated) {
			return nil, huma.Error404NotFound("Not found")
		}
		return nil, huma.Error400BadRequest("Failed to create prompt", err)
	}

	return &types.Response[promptmodels.PromptResponse]{Body: *createdPrompt}, nil
}

// RegisterPromptsCreateEndpoint registers POST /prompts (create or update; immediately visible).
func RegisterPromptsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService) {
	huma.Register(api, huma.Operation{
		OperationID: "create-prompt" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/prompts",
		Summary:     "Create or update prompt",
		Description: "Create a new prompt in the registry or update an existing one. Resources are immediately visible after creation.",
		Tags:        []string{"prompts"},
	}, func(ctx context.Context, input *CreatePromptInput) (*types.Response[promptmodels.PromptResponse], error) {
		return createPromptHandler(ctx, input, registry)
	})
}

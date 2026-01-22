package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// ListAgentsInput represents the input for listing agents
type ListAgentsInput struct {
	Cursor                 string  `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"agent-cursor-123"`
	Limit                  int     `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince           string  `query:"updated_since" json:"updated_since,omitempty" doc:"Filter agents updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search                 string  `query:"search" json:"search,omitempty" doc:"Search agents by name (substring match)" required:"false" example:"filesystem"`
	Version                string  `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
	Semantic               bool    `query:"semantic_search" json:"semantic_search,omitempty" doc:"Use semantic search for the search term"`
	SemanticMatchThreshold float64 `query:"semantic_threshold" json:"semantic_threshold,omitempty" doc:"Optional maximum cosine distance when semantic_search is enabled" required:"false"`
}

// AgentDetailInput represents the input for getting agent details
type AgentDetailInput struct {
	AgentName string `path:"agentName" json:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
}

// AgentVersionDetailInput represents the input for getting a specific version
type AgentVersionDetailInput struct {
	AgentName string `path:"agentName" json:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
	Version   string `path:"version" json:"version" doc:"URL-encoded agent version" example:"1.0.0"`
}

// AgentVersionsInput represents the input for listing all versions of an agent
type AgentVersionsInput struct {
	AgentName string `path:"agentName" json:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
}

// RegisterAgentsEndpoints registers all agent-related endpoints with a custom path prefix
// isAdmin: if true, shows all resources; if false, only shows published resources
func RegisterAgentsEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, isAdmin bool) {
	// Determine the tags based on whether this is admin or public
	tags := []string{"agents"}
	if isAdmin {
		tags = append(tags, "admin")
	}

	// List agents
	huma.Register(api, huma.Operation{
		OperationID: "list-agents" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/agents",
		Summary:     "List Agentic agents",
		Description: "Get a paginated list of Agentic agents from the registry",
		Tags:        tags,
	}, func(ctx context.Context, input *ListAgentsInput) (*Response[agentmodels.AgentListResponse], error) {
		// Build filter
		filter := &database.AgentFilter{}

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
		if input.Semantic {
			if strings.TrimSpace(input.Search) == "" {
				return nil, huma.Error400BadRequest("semantic_search requires the search parameter to be provided", nil)
			}
			filter.Semantic = &database.SemanticSearchOptions{
				RawQuery:  input.Search,
				Threshold: input.SemanticMatchThreshold,
			}
			filter.Semantic.HybridSubstring = filter.SubstringName
		}
		if input.Version != "" {
			if input.Version == "latest" {
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				filter.Version = &input.Version
			}
		}

		agents, nextCursor, err := registry.ListAgents(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest(err.Error(), err)
			}
			return nil, huma.Error500InternalServerError("Failed to get agents list", err)
		}

		agentValues := make([]agentmodels.AgentResponse, len(agents))
		for i, a := range agents {
			agentValues[i] = *a
		}
		return &Response[agentmodels.AgentListResponse]{
			Body: agentmodels.AgentListResponse{
				Agents: agentValues,
				Metadata: agentmodels.AgentMetadata{
					NextCursor: nextCursor,
					Count:      len(agents),
				},
			},
		}, nil
	})

	// Get specific agent version (supports "latest")
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}",
		Summary:     "Get specific Agentic agent version",
		Description: "Get detailed information about a specific version of an Agentic agent. Use the special version 'latest' to get the latest version.",
		Tags:        tags,
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*Response[agentmodels.AgentResponse], error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		var agentResp *agentmodels.AgentResponse
		if version == "latest" {
			agentResp, err = registry.GetAgentByName(ctx, agentName)
		} else {
			agentResp, err = registry.GetAgentByNameAndVersion(ctx, agentName, version)
		}
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get agent details", err)
		}
		return &Response[agentmodels.AgentResponse]{Body: *agentResp}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-agent-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodDelete,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}",
		Summary:     "Delete an agent version (admin)",
		Description: "Permanently delete a specific agent version from the registry. Admin only.",
		Tags:        tags,
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*Response[EmptyResponse], error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		if err := registry.DeleteAgent(ctx, agentName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			return nil, huma.Error500InternalServerError("Failed to delete agent", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{Message: "Agent deleted successfully"},
		}, nil
	})

	// Get all versions for an agent
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/agents/{agentName}/versions",
		Summary:     "Get all versions of an Agentic agent",
		Description: "Get all available versions for a specific Agentic agent",
		Tags:        tags,
	}, func(ctx context.Context, input *AgentVersionsInput) (*Response[agentmodels.AgentListResponse], error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}

		agents, err := registry.GetAllVersionsByAgentName(ctx, agentName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get agent versions", err)
		}

		agentValues := make([]agentmodels.AgentResponse, len(agents))
		for i, a := range agents {
			agentValues[i] = *a
		}
		return &Response[agentmodels.AgentListResponse]{
			Body: agentmodels.AgentListResponse{
				Agents: agentValues,
				Metadata: agentmodels.AgentMetadata{
					Count: len(agents),
				},
			},
		}, nil
	})
}

// CreateAgentInput represents the input for creating/updating an agent
type CreateAgentInput struct {
	Body agentmodels.AgentJSON `body:""`
}

// createAgentHandler is the shared handler logic for creating agents
func createAgentHandler(ctx context.Context, input *CreateAgentInput, registry service.RegistryService) (*Response[agentmodels.AgentResponse], error) {
	// Create/update the agent (published defaults to false in the service layer)
	createdAgent, err := registry.CreateAgent(ctx, &input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to create agent", err)
	}

	return &Response[agentmodels.AgentResponse]{Body: *createdAgent}, nil
}

// RegisterAgentsCreateEndpoint registers the public agents create/update endpoint at /agents/publish
// This endpoint creates or updates an agent in the registry (published defaults to false)
func RegisterAgentsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "create-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/publish",
		Summary:     "Create/update Agentic agent",
		Description: "Create a new Agentic agent in the registry or update an existing one. By default, agents are created as unpublished (published=false).",
		Tags:        []string{"agents", "publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *CreateAgentInput) (*Response[agentmodels.AgentResponse], error) {
		return createAgentHandler(ctx, input, registry)
	})

	// Also register a dedicated /agents/push endpoint for "push" operations that create an unpublished agent.
	huma.Register(api, huma.Operation{
		OperationID: "push-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/push",
		Summary:     "Push Agentic agent (create unpublished)",
		Description: "Create a new Agentic agent in the registry as an unpublished entry (published=false).",
		Tags:        []string{"agents", "publish"},
		Security:    []map[string][]string{{"bearer": {}}},
	}, func(ctx context.Context, input *CreateAgentInput) (*Response[agentmodels.AgentResponse], error) {
		return createAgentHandler(ctx, input, registry)
	})
}

// RegisterAdminAgentsCreateEndpoint registers the admin agents create/update endpoint at /agents
// This endpoint creates or updates an agent in the registry (published defaults to false)
func RegisterAdminAgentsCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-create-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents",
		Summary:     "Create/update Agentic agent (Admin)",
		Description: "Create a new Agentic agent in the registry or update an existing one. By default, agents are created as unpublished (published=false).",
		Tags:        []string{"agents", "admin"},
	}, func(ctx context.Context, input *CreateAgentInput) (*Response[agentmodels.AgentResponse], error) {
		// Create/update the agent (published defaults to false in the service layer)
		createdAgent, err := registry.CreateAgent(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to create agent", err)
		}

		return &Response[agentmodels.AgentResponse]{Body: *createdAgent}, nil
	})
}

// RegisterAgentsPublishStatusEndpoints registers the publish/unpublish status endpoints for agents
// These endpoints change the published status of existing agents
func RegisterAgentsPublishStatusEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	// Publish agent endpoint - marks an existing agent as published
	huma.Register(api, huma.Operation{
		OperationID: "publish-agent-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}/publish",
		Summary:     "Publish an existing agent",
		Description: "Mark an existing agent version as published, making it visible in public listings. This acts on an agent that was already created.",
		Tags:        []string{"agents", "admin"},
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the agent name and version
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to publish the agent
		if err := registry.PublishAgent(ctx, agentName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			return nil, huma.Error500InternalServerError("Failed to publish agent", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Agent published successfully",
			},
		}, nil
	})

	// Unpublish agent endpoint - marks an existing agent as unpublished
	huma.Register(api, huma.Operation{
		OperationID: "unpublish-agent-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}/unpublish",
		Summary:     "Unpublish an existing agent",
		Description: "Mark an existing agent version as unpublished, hiding it from public listings. This acts on an agent that was already created.",
		Tags:        []string{"agents", "admin"},
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the agent name and version
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to unpublish the agent
		if err := registry.UnpublishAgent(ctx, agentName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			return nil, huma.Error500InternalServerError("Failed to unpublish agent", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Agent unpublished successfully",
			},
		}, nil
	})
}

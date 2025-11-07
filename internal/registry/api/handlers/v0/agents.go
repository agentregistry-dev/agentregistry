package v0

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
)

// ListAgentsInput represents the input for listing agents
type ListAgentsInput struct {
	Cursor       string `query:"cursor" doc:"Pagination cursor" required:"false" example:"agent-cursor-123"`
	Limit        int    `query:"limit" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince string `query:"updated_since" doc:"Filter agents updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search       string `query:"search" doc:"Search agents by name (substring match)" required:"false" example:"filesystem"`
	Version      string `query:"version" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
}

// AgentDetailInput represents the input for getting agent details
type AgentDetailInput struct {
	AgentName string `path:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
}

// AgentVersionDetailInput represents the input for getting a specific version
type AgentVersionDetailInput struct {
	AgentName string `path:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
	Version   string `path:"version" doc:"URL-encoded agent version" example:"1.0.0"`
}

// AgentVersionsInput represents the input for listing all versions of an agent
type AgentVersionsInput struct {
	AgentName string `path:"agentName" doc:"URL-encoded agent name" example:"com.example%2Fmy-agent"`
}

// RegisterAgentsEndpoints registers all agent-related endpoints with a custom path prefix
func RegisterAgentsEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	// List agents
	huma.Register(api, huma.Operation{
		OperationID: "list-agents" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/agents",
		Summary:     "List Agentic agents",
		Description: "Get a paginated list of Agentic agents from the registry",
		Tags:        []string{"agents"},
	}, func(ctx context.Context, input *ListAgentsInput) (*Response[agentmodels.AgentListResponse], error) {
		// Build filter
		filter := &database.AgentFilter{}
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

		agents, nextCursor, err := registry.ListAgents(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
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
		Tags:        []string{"agents"},
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

	// Get all versions for an agent
	huma.Register(api, huma.Operation{
		OperationID: "get-agent-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/agents/{agentName}/versions",
		Summary:     "Get all versions of an Agentic agent",
		Description: "Get all available versions for a specific Agentic agent",
		Tags:        []string{"agents"},
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

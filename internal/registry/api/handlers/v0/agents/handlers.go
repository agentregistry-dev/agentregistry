package agents

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deploymentmeta"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	agentmodels "github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

const errRecordNotFound = "record not found"

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

func RegisterAgentsEndpoints(api huma.API, pathPrefix string, agentSvc agentsvc.Registry, deploymentSvc deploymentmeta.Lister) {
	tags := []string{"agents"}
	if strings.Contains(pathPrefix, "admin") {
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
	}, func(ctx context.Context, input *apitypes.ListAgentsInput) (*types.Response[agentmodels.AgentListResponse], error) {
		filter := &database.AgentFilter{}

		if input.UpdatedSince != "" {
			if updatedTime, err := time.Parse(time.RFC3339, input.UpdatedSince); err == nil {
				filter.UpdatedSince = &updatedTime
			} else {
				return nil, huma.Error400BadRequest("Invalid updated_since format: expected RFC3339 timestamp (e.g., 2025-08-07T13:15:04.280Z)")
			}
		}
		// When semantic search is active, use pure vector similarity instead of
		// AND-ing with a substring name filter.
		if input.Semantic {
			if strings.TrimSpace(input.Search) == "" {
				return nil, huma.Error400BadRequest("semantic_search requires the search parameter to be provided", nil)
			}
			filter.Semantic = &database.SemanticSearchOptions{
				RawQuery:  input.Search,
				Threshold: input.SemanticMatchThreshold,
			}
		} else if input.Search != "" {
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

		agents, nextCursor, err := agentSvc.ListAgents(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest(err.Error(), err)
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to get agents list", err)
		}

		agentValues := make([]agentmodels.AgentResponse, len(agents))
		for i, a := range agents {
			agentValues[i] = *a
		}
		agentValues = deploymentmeta.AttachAgentDeploymentMeta(ctx, deploymentSvc, agentValues)
		return &types.Response[agentmodels.AgentListResponse]{
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
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*types.Response[agentmodels.AgentResponse], error) {
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
			agentResp, err = agentSvc.GetAgent(ctx, agentName)
		} else {
			agentResp, err = agentSvc.GetAgentVersion(ctx, agentName, version)
		}
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to get agent details", err)
		}
		return &types.Response[agentmodels.AgentResponse]{
			Body: deploymentmeta.AttachAgentDeploymentMeta(
				ctx,
				deploymentSvc,
				[]agentmodels.AgentResponse{*agentResp},
			)[0],
		}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-agent-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodDelete,
		Path:        pathPrefix + "/agents/{agentName}/versions/{version}",
		Summary:     "Delete an agent version (admin)",
		Description: "Permanently delete a specific agent version from the registry. Admin only.",
		Tags:        tags,
	}, func(ctx context.Context, input *AgentVersionDetailInput) (*types.Response[types.EmptyResponse], error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		if err := agentSvc.DeleteAgent(ctx, agentName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to delete agent", err)
		}

		return &types.Response[types.EmptyResponse]{
			Body: types.EmptyResponse{Message: "Agent deleted successfully"},
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
	}, func(ctx context.Context, input *AgentVersionsInput) (*types.Response[agentmodels.AgentListResponse], error) {
		agentName, err := url.PathUnescape(input.AgentName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid agent name encoding", err)
		}

		agents, err := agentSvc.GetAgentVersions(ctx, agentName)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Agent not found")
			}
			if errors.Is(err, auth.ErrUnauthenticated) {
				return nil, huma.Error401Unauthorized("Authentication required")
			}
			if errors.Is(err, auth.ErrForbidden) {
				return nil, huma.Error403Forbidden("Forbidden")
			}
			return nil, huma.Error500InternalServerError("Failed to get agent versions", err)
		}

		agentValues := make([]agentmodels.AgentResponse, len(agents))
		for i, a := range agents {
			agentValues[i] = *a
		}
		agentValues = deploymentmeta.AttachAgentDeploymentMeta(ctx, deploymentSvc, agentValues)
		return &types.Response[agentmodels.AgentListResponse]{
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
func createAgentHandler(ctx context.Context, input *CreateAgentInput, agentSvc agentsvc.Registry, deploymentSvc deploymentmeta.Lister) (*types.Response[agentmodels.AgentResponse], error) {
	// Create/update the agent (published defaults to false in the service layer)
	createdAgent, err := agentSvc.PublishAgent(ctx, &input.Body)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil, huma.Error404NotFound("Not found")
		}
		if errors.Is(err, auth.ErrUnauthenticated) {
			return nil, huma.Error401Unauthorized("Authentication required")
		}
		if errors.Is(err, auth.ErrForbidden) {
			return nil, huma.Error403Forbidden("Forbidden")
		}
		return nil, huma.Error400BadRequest("Failed to create agent", err)
	}

	return &types.Response[agentmodels.AgentResponse]{
		Body: deploymentmeta.AttachAgentDeploymentMeta(
			ctx,
			deploymentSvc,
			[]agentmodels.AgentResponse{*createdAgent},
		)[0],
	}, nil
}

func RegisterAgentsCreateEndpoint(api huma.API, pathPrefix string, agentSvc agentsvc.Registry, deploymentSvc deploymentmeta.Lister) {
	huma.Register(api, huma.Operation{
		OperationID: "create-agent" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/agents",
		Summary:     "Create or update agent",
		Description: "Create a new Agentic agent in the registry or update an existing one. Resources are immediately visible after creation.",
		Tags:        []string{"agents"},
	}, func(ctx context.Context, input *CreateAgentInput) (*types.Response[agentmodels.AgentResponse], error) {
		return createAgentHandler(ctx, input, agentSvc, deploymentSvc)
	})
}

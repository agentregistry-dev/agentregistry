package registryserver

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	agentmodels "github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const (
	defaultPageLimit = 30
	maxPageLimit     = 100
)

// NewServer constructs an MCP server that exposes read-only discovery tools backed by the registry service.
// All endpoints are restricted to published content to keep the surface area safe for unauthenticated agents.
func NewServer(registry service.RegistryService) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    "agentregistry-mcp",
		Version: version.Version,
	}, &mcp.ServerOptions{
		HasTools: true,
	})

	addAgentTools(server, registry)
	addServerTools(server, registry)
	addSkillTools(server, registry)
	addMetaTools(server)

	return server
}

type listAgentsArgs struct {
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	UpdatedSince string `json:"updated_since,omitempty"`
	Search       string `json:"search,omitempty"`
	Version      string `json:"version,omitempty"`
}

func addAgentTools(server *mcp.Server, registry service.RegistryService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_agents",
		Description: "List published agents with optional search and pagination",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listAgentsArgs) (*mcp.CallToolResult, agentmodels.AgentListResponse, error) {
		filter := &database.AgentFilter{}
		published := true
		filter.Published = &published

		if args.UpdatedSince != "" {
			ts, err := time.Parse(time.RFC3339, args.UpdatedSince)
			if err != nil {
				return nil, agentmodels.AgentListResponse{}, fmt.Errorf("invalid updated_since: %w", err)
			}
			filter.UpdatedSince = &ts
		}
		if args.Search != "" {
			filter.SubstringName = &args.Search
		}
		if args.Version != "" {
			if args.Version == "latest" {
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				filter.Version = &args.Version
			}
		}

		limit := clampLimit(args.Limit)
		agents, nextCursor, err := registry.ListAgents(ctx, filter, args.Cursor, limit)
		if err != nil {
			return nil, agentmodels.AgentListResponse{}, err
		}

		out := agentmodels.AgentListResponse{
			Agents:   make([]agentmodels.AgentResponse, len(agents)),
			Metadata: agentmodels.AgentMetadata{NextCursor: nextCursor, Count: len(agents)},
		}
		for i, a := range agents {
			out.Agents[i] = *a
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_agent",
		Description: "Fetch a single published agent version (defaults to latest)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	}) (*mcp.CallToolResult, agentmodels.AgentResponse, error) {
		if args.Name == "" {
			return nil, agentmodels.AgentResponse{}, fmt.Errorf("name is required")
		}
		version := args.Version
		if version == "" {
			version = "latest"
		}

		var agent *agentmodels.AgentResponse
		var err error
		if version == "latest" {
			agent, err = registry.GetAgentByName(ctx, args.Name)
		} else {
			agent, err = registry.GetAgentByNameAndVersion(ctx, args.Name, version)
		}
		if err != nil {
			return nil, agentmodels.AgentResponse{}, err
		}
		return nil, *agent, nil
	})
}

type listServersArgs struct {
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	UpdatedSince string `json:"updated_since,omitempty"`
	Search       string `json:"search,omitempty"`
	Version      string `json:"version,omitempty"`
}

func addServerTools(server *mcp.Server, registry service.RegistryService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_servers",
		Description: "List published MCP servers with optional search and pagination",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listServersArgs) (*mcp.CallToolResult, apiv0.ServerListResponse, error) {
		filter := &database.ServerFilter{}
		published := true
		filter.Published = &published

		if args.UpdatedSince != "" {
			ts, err := time.Parse(time.RFC3339, args.UpdatedSince)
			if err != nil {
				return nil, apiv0.ServerListResponse{}, fmt.Errorf("invalid updated_since: %w", err)
			}
			filter.UpdatedSince = &ts
		}
		if args.Search != "" {
			filter.SubstringName = &args.Search
		}
		if args.Version != "" {
			if args.Version == "latest" {
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				filter.Version = &args.Version
			}
		}

		limit := clampLimit(args.Limit)
		servers, nextCursor, err := registry.ListServers(ctx, filter, args.Cursor, limit)
		if err != nil {
			return nil, apiv0.ServerListResponse{}, err
		}

		out := apiv0.ServerListResponse{
			Servers:  make([]apiv0.ServerResponse, len(servers)),
			Metadata: apiv0.Metadata{NextCursor: nextCursor, Count: len(servers)},
		}
		for i, s := range servers {
			out.Servers[i] = *s
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_server",
		Description: "Fetch a published MCP server version. Supports 'latest' or all versions.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
		All     bool   `json:"all_versions,omitempty"`
	}) (*mcp.CallToolResult, apiv0.ServerListResponse, error) {
		if args.Name == "" {
			return nil, apiv0.ServerListResponse{}, fmt.Errorf("name is required")
		}
		version := args.Version
		if version == "" {
			version = "latest"
		}

		publishedOnly := true
		if args.All {
			servers, err := registry.GetAllVersionsByServerName(ctx, args.Name, publishedOnly)
			if err != nil {
				return nil, apiv0.ServerListResponse{}, err
			}
			out := apiv0.ServerListResponse{
				Servers:  make([]apiv0.ServerResponse, len(servers)),
				Metadata: apiv0.Metadata{Count: len(servers)},
			}
			for i, s := range servers {
				out.Servers[i] = *s
			}
			return nil, out, nil
		}

		serverResp, err := fetchSingleServer(ctx, registry, args.Name, version, publishedOnly)
		if err != nil {
			return nil, apiv0.ServerListResponse{}, err
		}

		return nil, apiv0.ServerListResponse{
			Servers:  []apiv0.ServerResponse{*serverResp},
			Metadata: apiv0.Metadata{Count: 1},
		}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_server_readme",
		Description: "Fetch the README for a published server version (defaults to latest)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	}) (*mcp.CallToolResult, ServerReadmePayload, error) {
		if args.Name == "" {
			return nil, ServerReadmePayload{}, fmt.Errorf("name is required")
		}
		version := args.Version
		if version == "" {
			version = "latest"
		}

		var readme *database.ServerReadme
		var err error
		if version == "latest" {
			readme, err = registry.GetServerReadmeLatest(ctx, args.Name)
		} else {
			readme, err = registry.GetServerReadmeByVersion(ctx, args.Name, version)
		}
		if err != nil {
			return nil, ServerReadmePayload{}, err
		}

		return nil, ServerReadmePayload{
			Server:      readme.ServerName,
			Version:     readme.Version,
			Content:     string(readme.Content),
			ContentType: readme.ContentType,
			SizeBytes:   readme.SizeBytes,
			SHA256:      hex.EncodeToString(readme.SHA256),
			FetchedAt:   readme.FetchedAt,
		}, nil
	})
}

type listSkillsArgs struct {
	Cursor       string `json:"cursor,omitempty"`
	Limit        int    `json:"limit,omitempty"`
	UpdatedSince string `json:"updated_since,omitempty"`
	Search       string `json:"search,omitempty"`
	Version      string `json:"version,omitempty"`
}

func addSkillTools(server *mcp.Server, registry service.RegistryService) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "list_skills",
		Description: "List published skills with optional search and pagination",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args listSkillsArgs) (*mcp.CallToolResult, agentmodels.SkillListResponse, error) {
		filter := &database.SkillFilter{}
		published := true
		filter.Published = &published

		if args.UpdatedSince != "" {
			ts, err := time.Parse(time.RFC3339, args.UpdatedSince)
			if err != nil {
				return nil, agentmodels.SkillListResponse{}, fmt.Errorf("invalid updated_since: %w", err)
			}
			filter.UpdatedSince = &ts
		}
		if args.Search != "" {
			filter.SubstringName = &args.Search
		}
		if args.Version != "" {
			if args.Version == "latest" {
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				filter.Version = &args.Version
			}
		}

		limit := clampLimit(args.Limit)
		skills, nextCursor, err := registry.ListSkills(ctx, filter, args.Cursor, limit)
		if err != nil {
			return nil, agentmodels.SkillListResponse{}, err
		}

		out := agentmodels.SkillListResponse{
			Skills:   make([]agentmodels.SkillResponse, len(skills)),
			Metadata: agentmodels.SkillMetadata{NextCursor: nextCursor, Count: len(skills)},
		}
		for i, s := range skills {
			out.Skills[i] = *s
		}
		return nil, out, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "get_skill",
		Description: "Fetch a published skill version (defaults to latest)",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, args struct {
		Name    string `json:"name"`
		Version string `json:"version,omitempty"`
	}) (*mcp.CallToolResult, agentmodels.SkillResponse, error) {
		if args.Name == "" {
			return nil, agentmodels.SkillResponse{}, fmt.Errorf("name is required")
		}

		version := args.Version
		if version == "" {
			version = "latest"
		}

		var skill *agentmodels.SkillResponse
		var err error
		if version == "latest" {
			skill, err = registry.GetSkillByName(ctx, args.Name)
		} else {
			skill, err = registry.GetSkillByNameAndVersion(ctx, args.Name, version)
		}
		if err != nil {
			return nil, agentmodels.SkillResponse{}, err
		}
		return nil, *skill, nil
	})
}

func addMetaTools(server *mcp.Server) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "registry_health",
		Description: "Simple health check for the registry MCP bridge",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]string, error) {
		_ = ctx
		return nil, map[string]string{"status": "ok"}, nil
	})

	mcp.AddTool(server, &mcp.Tool{
		Name:        "registry_version",
		Description: "Return registry build metadata",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, map[string]string, error) {
		_ = ctx
		return nil, map[string]string{
			"version":    version.Version,
			"serverName": "agentregistry-mcp",
		}, nil
	})
}

// ServerReadmePayload is a compact representation of a server README blob.
type ServerReadmePayload struct {
	Server      string    `json:"server"`
	Version     string    `json:"version"`
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"`
	SizeBytes   int       `json:"size_bytes"`
	SHA256      string    `json:"sha256"`
	FetchedAt   time.Time `json:"fetched_at"`
}

func fetchSingleServer(ctx context.Context, registry service.RegistryService, name, version string, publishedOnly bool) (*apiv0.ServerResponse, error) {
	if version == "latest" {
		servers, err := registry.GetAllVersionsByServerName(ctx, name, publishedOnly)
		if err != nil {
			return nil, err
		}
		if len(servers) == 0 {
			return nil, errors.New("server not found")
		}
		for _, s := range servers {
			if s.Meta.Official != nil && s.Meta.Official.IsLatest {
				return s, nil
			}
		}
		return servers[0], nil
	}

	return registry.GetServerByNameAndVersion(ctx, name, version, publishedOnly)
}

func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultPageLimit
	}
	if limit > maxPageLimit {
		return maxPageLimit
	}
	return limit
}

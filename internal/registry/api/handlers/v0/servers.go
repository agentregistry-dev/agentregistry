package v0

import (
	"context"
	"encoding/hex"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

const errRecordNotFound = "record not found"
const semanticMetadataKey = "agentregistry.solo.io/semantic"

// normalizeServerResponse moves semantic metadata into a dedicated response meta
// field while keeping publisher-provided data untouched.
func normalizeServerResponse(src *apiv0.ServerResponse) models.ServerResponse {
	if src == nil {
		return models.ServerResponse{}
	}

	server := src.Server
	var semanticScore *float64

	if server.Meta != nil && server.Meta.PublisherProvided != nil {
		if raw, ok := server.Meta.PublisherProvided[semanticMetadataKey]; ok {
			if m, okm := raw.(map[string]any); okm {
				if v, okv := m["score"].(float64); okv {
					semanticScore = &v
				}
			}
			// Remove semantic metadata from publisher-provided to avoid mixing concerns.
			delete(server.Meta.PublisherProvided, semanticMetadataKey)
			if len(server.Meta.PublisherProvided) == 0 {
				server.Meta.PublisherProvided = nil
			}
		}
	}

	meta := models.ServerResponseMeta{
		Official: src.Meta.Official,
	}
	if semanticScore != nil {
		meta.Semantic = &models.ServerSemanticMeta{Score: *semanticScore}
	}

	return models.ServerResponse{
		Server: server,
		Meta:   meta,
	}
}

// ListServersInput represents the input for listing servers
type ListServersInput struct {
	Cursor                 string  `query:"cursor" json:"cursor,omitempty" doc:"Pagination cursor" required:"false" example:"server-cursor-123"`
	Limit                  int     `query:"limit" json:"limit,omitempty" doc:"Number of items per page" default:"30" minimum:"1" maximum:"100" example:"50"`
	UpdatedSince           string  `query:"updated_since" json:"updated_since,omitempty" doc:"Filter servers updated since timestamp (RFC3339 datetime)" required:"false" example:"2025-08-07T13:15:04.280Z"`
	Search                 string  `query:"search" json:"search,omitempty" doc:"Search servers by name (substring match)" required:"false" example:"filesystem"`
	Version                string  `query:"version" json:"version,omitempty" doc:"Filter by version ('latest' for latest version, or an exact version like '1.2.3')" required:"false" example:"latest"`
	Semantic               bool    `query:"semantic_search" json:"semantic_search,omitempty" doc:"Use semantic search for the search term (hybrid with substring filter when search is set)" default:"false"`
	SemanticMatchThreshold float64 `query:"semantic_threshold" json:"semantic_threshold,omitempty" doc:"Optional maximum distance for semantic matches (cosine distance)" required:"false"`
}

// ServerDetailInput represents the input for getting server details
type ServerDetailInput struct {
	ServerName string `path:"serverName" json:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
}

// ServerVersionDetailInput represents the input for getting a specific version
type ServerVersionDetailInput struct {
	ServerName    string `path:"serverName" json:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
	Version       string `path:"version" json:"version" doc:"URL-encoded server version" example:"1.0.0"`
	All           bool   `query:"all" json:"all,omitempty" doc:"If true, return all versions of the server instead of a single version" default:"false"`
	PublishedOnly bool   `query:"published_only" json:"published_only,omitempty" doc:"If true, only return published versions (only applies when all=true)" default:"false"`
}

// ServerVersionsInput represents the input for listing all versions of a server
type ServerVersionsInput struct {
	ServerName string `path:"serverName" json:"serverName" doc:"URL-encoded server name" example:"com.example%2Fmy-server"`
}

// ServerReadmeResponse is the payload for README fetch endpoints
type ServerReadmeResponse struct {
	Content     string    `json:"content"`
	ContentType string    `json:"content_type"`
	SizeBytes   int       `json:"size_bytes"`
	Sha256      string    `json:"sha256"`
	Version     string    `json:"version"`
	FetchedAt   time.Time `json:"fetched_at"`
}

// RegisterServersEndpoints registers all server-related endpoints with a custom path prefix
// isAdmin: if true, shows all resources; if false, only shows published resources
func RegisterServersEndpoints(api huma.API, pathPrefix string, registry service.RegistryService, isAdmin bool) {
	if isAdmin {
		huma.Register(api, huma.Operation{
			OperationID: "delete-server-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
			Method:      http.MethodDelete,
			Path:        pathPrefix + "/servers/{serverName}/versions/{version}",
			Summary:     "Delete MCP server version",
			Description: "Permanently delete an MCP server version from the registry.",
			Tags:        []string{"servers", "admin"},
		}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[EmptyResponse], error) {
			serverName, err := url.PathUnescape(input.ServerName)
			if err != nil {
				return nil, huma.Error400BadRequest("Invalid server name encoding", err)
			}
			version, err := url.PathUnescape(input.Version)
			if err != nil {
				return nil, huma.Error400BadRequest("Invalid version encoding", err)
			}
			if err := registry.DeleteServer(ctx, serverName, version); err != nil {
				if errors.Is(err, database.ErrNotFound) {
					return nil, huma.Error404NotFound("Server not found")
				}
				return nil, huma.Error500InternalServerError("Failed to delete server", err)
			}
			return &Response[EmptyResponse]{
				Body: EmptyResponse{
					Message: "Server deleted successfully",
				},
			}, nil
		})
	}
	var tags []string
	tags = []string{"servers"}
	if isAdmin {
		tags = append(tags, "admin")
	}

	// Register /servers/push endpoint here
	huma.Register(api, huma.Operation{
		OperationID: "push-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/servers/push",
		Summary:     "Push MCP server (create unpublished)",
		Description: "Create a new MCP server in the registry as an unpublished entry (published=false).",
		Tags:        tags,
	}, func(ctx context.Context, input *CreateServerInput) (*Response[models.ServerResponse], error) {
		// Always create as unpublished (handled in service layer)
		return createServerHandler(ctx, input, registry)
	})
	// Determine the tags based on whether this is admin or public
	tags = []string{"servers"}
	if isAdmin {
		tags = append(tags, "admin")
	}

	// List servers endpoint
	huma.Register(api, huma.Operation{
		OperationID: "list-servers" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers",
		Summary:     "List MCP servers",
		Description: "Get a paginated list of MCP servers from the registry",
		Tags:        tags,
	}, func(ctx context.Context, input *ListServersInput) (*Response[models.ServerListResponse], error) {
		// Build filter from input parameters
		filter := &database.ServerFilter{}

		// For public endpoints, only show published resources
		if !isAdmin {
			published := true
			filter.Published = &published
		}

		// Parse updated_since parameter
		if input.UpdatedSince != "" {
			// Parse RFC3339 format
			if updatedTime, err := time.Parse(time.RFC3339, input.UpdatedSince); err == nil {
				filter.UpdatedSince = &updatedTime
			} else {
				return nil, huma.Error400BadRequest("Invalid updated_since format: expected RFC3339 timestamp (e.g., 2025-08-07T13:15:04.280Z)")
			}
		}

		// Handle search parameter
		if input.Search != "" {
			filter.SubstringName = &input.Search
		}

		if input.Semantic {
			if strings.TrimSpace(input.Search) == "" {
				return nil, huma.Error400BadRequest("semantic_search requires the search parameter to be set", nil)
			}
			filter.Semantic = &database.SemanticSearchOptions{
				RawQuery:  input.Search,
				Threshold: input.SemanticMatchThreshold,
			}
			filter.Semantic.HybridSubstring = filter.SubstringName
		}

		// Handle version parameter
		if input.Version != "" {
			if input.Version == "latest" {
				// Special case: filter for latest versions
				isLatest := true
				filter.IsLatest = &isLatest
			} else {
				// Future: exact version matching
				filter.Version = &input.Version
			}
		}

		// Get paginated results with filtering
		servers, nextCursor, err := registry.ListServers(ctx, filter, input.Cursor, input.Limit)
		if err != nil {
			if errors.Is(err, database.ErrInvalidInput) {
				return nil, huma.Error400BadRequest(err.Error(), err)
			}
			return nil, huma.Error500InternalServerError("Failed to get registry list", err)
		}

		// Convert []*ServerResponse to []ServerResponse while normalizing metadata.
		serverValues := make([]models.ServerResponse, len(servers))
		for i, server := range servers {
			serverValues[i] = normalizeServerResponse(server)
		}

		return &Response[models.ServerListResponse]{
			Body: models.ServerListResponse{
				Servers: serverValues,
				Metadata: models.ServerMetadata{
					NextCursor: nextCursor,
					Count:      len(servers),
				},
			},
		}, nil
	})

	// Get specific server version endpoint
	// Can return a single version or all versions based on query parameters
	huma.Register(api, huma.Operation{
		OperationID: "get-server-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}",
		Summary:     "Get specific MCP server version",
		Description: "Get detailed information about a specific version of an MCP server. Set 'all=true' query parameter to get all versions. Set 'published_only=true' to filter to only published versions (only applies when all=true).",
		Tags:        tags,
	}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[models.ServerListResponse], error) {
		// URL-decode the server name
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		// URL-decode the version
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// If all=true, return all versions
		if input.All {
			// Determine if we should filter to published only
			onlyPublished := input.PublishedOnly
			// For public endpoints, always filter to published only
			if !isAdmin {
				onlyPublished = true
			}

			servers, err := registry.GetAllVersionsByServerName(ctx, serverName, onlyPublished)
			if err != nil {
				if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
					return nil, huma.Error404NotFound("Server not found")
				}
				return nil, huma.Error500InternalServerError("Failed to get server versions", err)
			}

			// Convert []*ServerResponse to []ServerResponse
			serverValues := make([]models.ServerResponse, len(servers))
			for i, server := range servers {
				serverValues[i] = normalizeServerResponse(server)
			}

			return &Response[models.ServerListResponse]{
				Body: models.ServerListResponse{
					Servers: serverValues,
					Metadata: models.ServerMetadata{
						Count: len(servers),
					},
				},
			}, nil
		}

		// Default behavior: return a single version (wrapped in a list for consistency)
		// For public endpoints, always filter to published only
		publishedOnly := input.PublishedOnly
		if !isAdmin {
			publishedOnly = true
		}

		var serverResponse *apiv0.ServerResponse

		// Handle "latest" as a special version string
		if version == "latest" {
			// Get all versions and find the latest one
			servers, err := registry.GetAllVersionsByServerName(ctx, serverName, publishedOnly)
			if err != nil {
				if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
					return nil, huma.Error404NotFound("Server not found")
				}
				return nil, huma.Error500InternalServerError("Failed to get server versions", err)
			}
			if len(servers) == 0 {
				return nil, huma.Error404NotFound("Server not found")
			}
			// Find the latest version (should be marked with IsLatest=true)
			var latestServer *apiv0.ServerResponse
			for _, s := range servers {
				if s.Meta.Official != nil && s.Meta.Official.IsLatest {
					latestServer = s
					break
				}
			}
			// If no server is marked as latest, use the first one (shouldn't happen, but be defensive)
			if latestServer == nil {
				latestServer = servers[0]
			}
			serverResponse = latestServer
		} else {
			serverResponse, err = registry.GetServerByNameAndVersion(ctx, serverName, version, publishedOnly)
			if err != nil {
				if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
					return nil, huma.Error404NotFound("Server not found")
				}
				return nil, huma.Error500InternalServerError("Failed to get server details", err)
			}
		}

		// Return single server wrapped in a list response
		return &Response[models.ServerListResponse]{
			Body: models.ServerListResponse{
				Servers: []models.ServerResponse{normalizeServerResponse(serverResponse)},
				Metadata: models.ServerMetadata{
					Count: 1,
				},
			},
		}, nil
	})

	// Get server versions endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-versions" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/versions",
		Summary:     "Get all versions of an MCP server",
		Description: "Get all available versions for a specific MCP server",
		Tags:        tags,
	}, func(ctx context.Context, input *ServerVersionsInput) (*Response[models.ServerListResponse], error) {
		// URL-decode the server name
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		// Get all versions for this server
		// For public endpoints, only get published versions (published = true)
		// For admin endpoints, get all versions (published = true or false)
		servers, err := registry.GetAllVersionsByServerName(ctx, serverName, !isAdmin)
		if err != nil {
			if err.Error() == errRecordNotFound || errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to get server versions", err)
		}

		// Convert []*ServerResponse to []ServerResponse
		serverValues := make([]models.ServerResponse, len(servers))
		for i, server := range servers {
			serverValues[i] = normalizeServerResponse(server)
		}

		return &Response[models.ServerListResponse]{
			Body: models.ServerListResponse{
				Servers: serverValues,
				Metadata: models.ServerMetadata{
					Count: len(servers),
				},
			},
		}, nil
	})

	// Get latest server README endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-readme" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/readme",
		Summary:     "Get server README",
		Description: "Fetch the README markdown document for the latest version of a server",
		Tags:        tags,
	}, func(ctx context.Context, input *ServerDetailInput) (*Response[ServerReadmeResponse], error) {
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}

		readme, err := registry.GetServerReadmeLatest(ctx, serverName)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("README not found")
			}
			return nil, huma.Error500InternalServerError("Failed to fetch server README", err)
		}

		return &Response[ServerReadmeResponse]{
			Body: toServerReadmeResponse(readme),
		}, nil
	})

	// Get specific version README endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-version-readme" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}/readme",
		Summary:     "Get server README for a version",
		Description: "Fetch the README markdown document for a specific server version",
		Tags:        tags,
	}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[ServerReadmeResponse], error) {
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		var readme *database.ServerReadme
		if version == "latest" {
			readme, err = registry.GetServerReadmeLatest(ctx, serverName)
		} else {
			readme, err = registry.GetServerReadmeByVersion(ctx, serverName, version)
		}
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("README not found")
			}
			return nil, huma.Error500InternalServerError("Failed to fetch server README", err)
		}

		return &Response[ServerReadmeResponse]{
			Body: toServerReadmeResponse(readme),
		}, nil
	})
}

func toServerReadmeResponse(readme *database.ServerReadme) ServerReadmeResponse {
	shaValue := ""
	if len(readme.SHA256) > 0 {
		shaValue = hex.EncodeToString(readme.SHA256)
	}
	return ServerReadmeResponse{
		Content:     string(readme.Content),
		ContentType: readme.ContentType,
		SizeBytes:   readme.SizeBytes,
		Sha256:      shaValue,
		Version:     readme.Version,
		FetchedAt:   readme.FetchedAt,
	}
}

// CreateServerInput represents the input for creating/updating a server
type CreateServerInput struct {
	Body apiv0.ServerJSON `body:""`
}

// createServerHandler is the shared handler logic for creating servers
func createServerHandler(ctx context.Context, input *CreateServerInput, registry service.RegistryService) (*Response[models.ServerResponse], error) {
	// Create/update the server (published defaults to false in the service layer)
	createdServer, err := registry.CreateServer(ctx, &input.Body)
	if err != nil {
		return nil, huma.Error400BadRequest("Failed to create server", err)
	}

	return &Response[models.ServerResponse]{
		Body: normalizeServerResponse(createdServer),
	}, nil
}

// RegisterCreateEndpoint registers the public create/update server endpoint at /publish
// This endpoint creates or updates a server in the registry (published defaults to false)
func RegisterCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "create-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/publish",
		Summary:     "Create/update MCP server",
		Description: "Create a new MCP server in the registry or update an existing one. By default, servers are created as unpublished (published=false).",
		Tags:        []string{"servers", "publish"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *CreateServerInput) (*Response[models.ServerResponse], error) {
		return createServerHandler(ctx, input, registry)
	})
}

// RegisterAdminCreateEndpoint registers the admin create/update server endpoint at /servers
// This endpoint creates or updates a server in the registry (published defaults to false)
func RegisterAdminCreateEndpoint(api huma.API, pathPrefix string, registry service.RegistryService) {
	huma.Register(api, huma.Operation{
		OperationID: "admin-create-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/servers",
		Summary:     "Create/update MCP server (Admin)",
		Description: "Create a new MCP server in the registry or update an existing one. By default, servers are created as unpublished (published=false).",
		Tags:        []string{"servers", "admin"},
	}, func(ctx context.Context, input *CreateServerInput) (*Response[models.ServerResponse], error) {
		return createServerHandler(ctx, input, registry)
	})
}

// RegisterPublishStatusEndpoints registers the publish/unpublish status endpoints for servers
// These endpoints change the published status of existing servers
func RegisterPublishStatusEndpoints(api huma.API, pathPrefix string, registry service.RegistryService) {
	// Publish server endpoint - marks an existing server as published
	huma.Register(api, huma.Operation{
		OperationID: "publish-server-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}/publish",
		Summary:     "Publish an existing server",
		Description: "Mark an existing server version as published, making it visible in public listings. This acts on a server that was already created.",
		Tags:        []string{"servers", "admin"},
	}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the server name and version
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to publish the server
		if err := registry.PublishServer(ctx, serverName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to publish server", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Server published successfully",
			},
		}, nil
	})

	// Unpublish server endpoint - marks an existing server as unpublished
	huma.Register(api, huma.Operation{
		OperationID: "unpublish-server-status" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/servers/{serverName}/versions/{version}/unpublish",
		Summary:     "Unpublish an existing server",
		Description: "Mark an existing server version as unpublished, hiding it from public listings. This acts on a server that was already created.",
		Tags:        []string{"servers", "admin"},
	}, func(ctx context.Context, input *ServerVersionDetailInput) (*Response[EmptyResponse], error) {
		// URL-decode the server name and version
		serverName, err := url.PathUnescape(input.ServerName)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid server name encoding", err)
		}
		version, err := url.PathUnescape(input.Version)
		if err != nil {
			return nil, huma.Error400BadRequest("Invalid version encoding", err)
		}

		// Call the service to unpublish the server
		if err := registry.UnpublishServer(ctx, serverName, version); err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, huma.Error404NotFound("Server not found")
			}
			return nil, huma.Error500InternalServerError("Failed to unpublish server", err)
		}

		return &Response[EmptyResponse]{
			Body: EmptyResponse{
				Message: "Server unpublished successfully",
			},
		}, nil
	})
}

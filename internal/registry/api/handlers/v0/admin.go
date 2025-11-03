package v0

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/importer"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// ImportRequest represents the input for importing servers
type ImportRequest struct {
	Source         string            `json:"source" doc:"Source URL or file path" example:"https://registry.example.com/v0/servers"`
	Headers        map[string]string `json:"headers,omitempty" doc:"Optional HTTP headers"`
	Update         bool              `json:"update,omitempty" doc:"Update existing entries" default:"false"`
	SkipValidation bool              `json:"skip_validation,omitempty" doc:"Skip validation" default:"false"`
}

// ImportInput represents the full input including the body
type ImportInput struct {
	Body ImportRequest `body:""`
}

// ImportResponse represents the response from an import operation
type ImportResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// ServerStatsResponse represents statistics about the registry
type ServerStatsResponse struct {
	TotalServers      int `json:"total_servers"`
	TotalServerNames  int `json:"total_server_names"`
	ActiveServers     int `json:"active_servers"`
	DeprecatedServers int `json:"deprecated_servers"`
	DeletedServers    int `json:"deleted_servers"`
}

// CreateServerInput represents the input for creating a server
type CreateServerInput struct {
	Body apiv0.ServerJSON `body:""`
}

// RegisterAdminEndpoints registers admin endpoints
func RegisterAdminEndpoints(api huma.API, pathPrefix string, registryService service.RegistryService, cfg *config.Config) {
	// Import endpoint (synchronous)
	huma.Register(api, huma.Operation{
		OperationID: "import-servers" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/admin/import",
		Summary:     "Import servers from external registry",
		Description: "Import MCP servers from an external registry or seed file.",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *ImportInput) (*Response[ImportResponse], error) {
		if strings.TrimSpace(input.Body.Source) == "" {
			return nil, huma.Error400BadRequest("source is required")
		}

		// Create HTTP client with longer timeout for imports
		httpClient := &http.Client{Timeout: 5 * time.Minute}

		// Create importer service
		importerService := importer.NewService(registryService)
		importerService.SetHTTPClient(httpClient)
		importerService.SetRequestHeaders(input.Body.Headers)
		importerService.SetUpdateIfExists(input.Body.Update)

		// Run import
		err := importerService.ImportFromPath(ctx, input.Body.Source)

		if err != nil {
			return &Response[ImportResponse]{
				Body: ImportResponse{
					Success: false,
					Message: err.Error(),
				},
			}, nil
		}

		return &Response[ImportResponse]{
			Body: ImportResponse{
				Success: true,
				Message: "Import completed successfully",
			},
		}, nil
	})

	// Create server endpoint (admin-only, no auth required)
	huma.Register(api, huma.Operation{
		OperationID: "create-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/admin/servers",
		Summary:     "Create a new server",
		Description: "Create a new MCP server in the registry (admin-only endpoint)",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *CreateServerInput) (*Response[apiv0.ServerResponse], error) {
		// Validate required fields
		if strings.TrimSpace(input.Body.Name) == "" {
			return nil, huma.Error400BadRequest("server name is required")
		}
		if strings.TrimSpace(input.Body.Version) == "" {
			return nil, huma.Error400BadRequest("server version is required")
		}
		if strings.TrimSpace(input.Body.Description) == "" {
			return nil, huma.Error400BadRequest("server description is required")
		}

		// Create the server using the registry service
		publishedServer, err := registryService.CreateServer(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to create server", err)
		}

		return &Response[apiv0.ServerResponse]{
			Body: *publishedServer,
		}, nil
	})

	// Stats endpoint
	huma.Register(api, huma.Operation{
		OperationID: "get-server-stats" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/admin/stats",
		Summary:     "Get registry statistics",
		Description: "Get statistics about servers in the registry",
		Tags:        []string{"admin"},
	}, func(ctx context.Context, input *struct{}) (*Response[ServerStatsResponse], error) {
		// Get all servers (with a high limit to count them)
		servers, _, err := registryService.ListServers(ctx, &database.ServerFilter{}, "", 10000)
		if err != nil {
			return nil, huma.Error500InternalServerError("Failed to get statistics", err)
		}

		// Calculate stats
		stats := ServerStatsResponse{
			TotalServers: len(servers),
		}

		// Count by status and unique names
		uniqueNames := make(map[string]bool)
		for _, server := range servers {
			uniqueNames[server.Server.Name] = true
			if server.Meta.Official != nil {
				switch server.Meta.Official.Status {
				case "active":
					stats.ActiveServers++
				case "deprecated":
					stats.DeprecatedServers++
				case "deleted":
					stats.DeletedServers++
				}
			}
		}
		stats.TotalServerNames = len(uniqueNames)

		return &Response[ServerStatsResponse]{
			Body: stats,
		}, nil
	})
}

package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/danielgtaylor/huma/v2"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// PublishServerInput represents the input for publishing a server
type PublishServerInput struct {
	Body apiv0.ServerJSON `body:""`
}

// RegisterPublishEndpoint registers the publish endpoint with a custom path prefix
func RegisterPublishEndpoint(api huma.API, pathPrefix string, registry service.RegistryService, authz auth.Authorizer) {
	huma.Register(api, huma.Operation{
		OperationID: "publish-server" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodPost,
		Path:        pathPrefix + "/publish",
		Summary:     "Publish MCP server",
		Description: "Publish a new MCP server to the registry or update an existing one",
		Tags:        []string{"publish"},
		Security: []map[string][]string{
			{"bearer": {}},
		},
	}, func(ctx context.Context, input *PublishServerInput) (*Response[apiv0.ServerResponse], error) {
		if err := authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{Name: input.Body.Name, Type: "server"}); err != nil {
			return nil, err
		}

		// Publish the server with extensions
		publishedServer, err := registry.CreateServer(ctx, &input.Body)
		if err != nil {
			return nil, huma.Error400BadRequest("Failed to publish server", err)
		}

		// Return the published server response with metadata
		return &Response[apiv0.ServerResponse]{
			Body: *publishedServer,
		}, nil
	})
}

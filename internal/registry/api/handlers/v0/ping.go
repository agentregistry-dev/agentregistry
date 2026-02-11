package v0

import (
	"context"
	"net/http"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

// PingBody represents the ping response body
type PingBody struct {
	Pong bool `json:"pong" example:"true" doc:"Ping response"`
}

// RegisterPingEndpoint registers the ping endpoint with a custom path prefix
func RegisterPingEndpoint(api huma.API, pathPrefix string) {
	huma.Register(api, huma.Operation{
		OperationID: "ping" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/ping",
		Summary:     "Ping",
		Description: "Simple ping endpoint",
		Tags:        []string{"ping"},
	}, func(_ context.Context, _ *struct{}) (*types.Response[PingBody], error) {
		return &types.Response[PingBody]{
			Body: PingBody{
				Pong: true,
			},
		}, nil
	})
}

package version

import (
	"context"
	"net/http"
	"strings"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
)

type VersionBody = arv0.VersionBody

func RegisterVersionEndpoint(api huma.API, pathPrefix string, versionInfo *VersionBody) {
	huma.Register(api, huma.Operation{
		OperationID: "get-version" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/version",
		Summary:     "Get version information",
		Description: "Returns the version, git commit, and build time of the registry application",
		Tags:        []string{"version"},
	}, func(_ context.Context, _ *struct{}) (*types.Response[VersionBody], error) {
		return &types.Response[VersionBody]{
			Body: *versionInfo,
		}, nil
	})
}

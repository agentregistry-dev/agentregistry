package api

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
)

// VersionResponse represents the version information
type VersionResponse struct {
	Body struct {
		Version   string `json:"version" example:"1.0.0" doc:"Version of the API"`
		BuildTime string `json:"buildTime" example:"2024-01-01T00:00:00Z" doc:"Build timestamp"`
		GitCommit string `json:"gitCommit" example:"abc123" doc:"Git commit hash"`
	}
}

// RegisterVersionEndpoint registers the version endpoint
func RegisterVersionEndpoint(api huma.API, basePath string, version, buildTime, gitCommit string) {
	huma.Register(api, huma.Operation{
		OperationID: "get-api-version",
		Method:      http.MethodGet,
		Path:        basePath + "/version",
		Summary:     "Get API version information",
		Description: "Returns version, build time, and git commit information",
		Tags:        []string{"version"},
	}, func(ctx context.Context, input *struct{}) (*VersionResponse, error) {
		resp := &VersionResponse{}
		resp.Body.Version = version
		resp.Body.BuildTime = buildTime
		resp.Body.GitCommit = gitCommit
		return resp, nil
	})
}

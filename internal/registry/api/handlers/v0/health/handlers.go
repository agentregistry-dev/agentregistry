package health

import (
	"context"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// HealthBody represents the health check response body.
type HealthBody struct {
	Status         string `json:"status" example:"ok" doc:"Health status"`
	GitHubClientID string `json:"github_client_id,omitempty" doc:"GitHub OAuth App Client ID"`
	PlatformMode   string `json:"platform_mode,omitempty" example:"docker" doc:"Platform mode" enum:"docker,kubernetes"`
}

func RegisterHealthEndpoint(api huma.API, pathPrefix string, cfg *config.Config, metrics *telemetry.Metrics) {
	huma.Register(api, huma.Operation{
		OperationID: "get-health" + strings.ReplaceAll(pathPrefix, "/", "-"),
		Method:      http.MethodGet,
		Path:        pathPrefix + "/health",
		Summary:     "Health check",
		Description: "Check the health status of the API",
		Tags:        []string{"health"},
	}, func(ctx context.Context, _ *struct{}) (*types.Response[HealthBody], error) {
		recordHealthMetrics(ctx, metrics, pathPrefix+"/health", cfg.Version)

		return &types.Response[HealthBody]{
			Body: HealthBody{
				Status:         "ok",
				GitHubClientID: cfg.GithubClientID,
				PlatformMode:   cfg.PlatformMode,
			},
		}, nil
	})
}

func recordHealthMetrics(ctx context.Context, metrics *telemetry.Metrics, path string, version string) {
	attrs := []attribute.KeyValue{
		attribute.String("path", path),
		attribute.String("version", version),
		attribute.String("service", telemetry.Namespace),
	}

	metrics.Up.Record(ctx, 1, metric.WithAttributes(attrs...))
}

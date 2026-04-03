package health_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

func TestHealthEndpoint(t *testing.T) {
	testCases := []struct {
		name           string
		config         *config.Config
		expectedStatus int
		expectedBody   v0health.HealthBody
	}{
		{
			name: "returns health status with github client id",
			config: &config.Config{
				GithubClientID: "test-github-client-id",
			},
			expectedStatus: http.StatusOK,
			expectedBody: v0health.HealthBody{
				Status:         "ok",
				GitHubClientID: "test-github-client-id",
			},
		},
		{
			name: "returns health status without github client id",
			config: &config.Config{
				GithubClientID: "",
			},
			expectedStatus: http.StatusOK,
			expectedBody: v0health.HealthBody{
				Status:         "ok",
				GitHubClientID: "",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			shutdownTelemetry, metrics, _ := telemetry.InitMetrics("test")

			v0health.RegisterHealthEndpoint(api, "/v0", tc.config, metrics)

			req := httptest.NewRequest(http.MethodGet, "/v0/health", nil)
			w := httptest.NewRecorder()

			mux.ServeHTTP(w, req)

			_ = shutdownTelemetry(context.Background())

			assert.Equal(t, tc.expectedStatus, w.Code)

			body := w.Body.String()
			assert.Contains(t, body, `"status":"ok"`)

			if tc.config.GithubClientID != "" {
				assert.Contains(t, body, `"github_client_id":"test-github-client-id"`)
			} else {
				assert.NotContains(t, body, `"github_client_id"`)
			}
		})
	}
}

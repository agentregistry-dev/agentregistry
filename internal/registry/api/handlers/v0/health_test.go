package v0_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

func TestHealthEndpoint(t *testing.T) {
	testCases := []struct {
		name           string
		config         *config.Config
		pingErr        error
		expectedStatus int
		expectedBody   v0.HealthBody
	}{
		{
			name: "returns health status with github client id",
			config: &config.Config{
				GithubClientID: "test-github-client-id",
			},
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthBody{
				Status:         "ok",
				Database:       "ok",
				GitHubClientID: "test-github-client-id",
			},
		},
		{
			name: "returns health status without github client id",
			config: &config.Config{
				GithubClientID: "",
			},
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthBody{
				Status:   "ok",
				Database: "ok",
			},
		},
		{
			name: "returns degraded when database is unavailable",
			config: &config.Config{
				GithubClientID: "",
			},
			pingErr:        errors.New("connection refused"),
			expectedStatus: http.StatusOK,
			expectedBody: v0.HealthBody{
				Status:   "degraded",
				Database: "unavailable",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			mux := http.NewServeMux()
			api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))

			shutdownTelemetry, metrics, _ := telemetry.InitMetrics("test")

			fakeRegistry := servicetesting.NewFakeRegistry()
			if tc.pingErr != nil {
				fakeRegistry.PingDBFn = func(_ context.Context) error { return tc.pingErr }
			}
			v0.RegisterHealthEndpoint(api, "/v0", tc.config, metrics, fakeRegistry)

			// Create a test request
			req := httptest.NewRequest(http.MethodGet, "/v0/health", nil)
			w := httptest.NewRecorder()

			// Serve the request
			mux.ServeHTTP(w, req)

			// shut down the metric provider
			_ = shutdownTelemetry(context.Background())

			// Check the status code
			assert.Equal(t, tc.expectedStatus, w.Code)

			// Check the response body
			// Since Huma adds a $schema field, we'll check individual fields
			body := w.Body.String()
			assert.Contains(t, body, `"status":"`+tc.expectedBody.Status+`"`)
			assert.Contains(t, body, `"database":"`+tc.expectedBody.Database+`"`)

			if tc.config.GithubClientID != "" {
				assert.Contains(t, body, `"github_client_id":"test-github-client-id"`)
			} else {
				assert.NotContains(t, body, `"github_client_id"`)
			}
		})
	}
}

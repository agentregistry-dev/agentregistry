package servers_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	v0health "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/health"
	v0servers "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/servers"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func TestPrometheusHandler(t *testing.T) {
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)
	testConfig := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // Disable for unit tests
	}

	storeDB := database.NewTestDB(t)
	serverService := serversvc.New(serversvc.Dependencies{StoreDB: storeDB, Config: testConfig})
	deploymentService := deploymentsvc.New(deploymentsvc.Dependencies{StoreDB: storeDB})
	server, err := serverService.PublishServer(context.Background(), &apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "io.github.example/test-server",
		Description: "Test server detail",
		Repository: &model.Repository{
			URL:    "https://github.com/example/test-server",
			Source: "git",
			ID:     "example/test-server",
		},
		Version: "2.0.0",
	})
	require.NoError(t, err)

	cfg := testConfig
	shutdownTelemetry, metrics, _ := telemetry.InitMetrics("dev")

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	// Add metrics middleware with options
	api.UseMiddleware(router.MetricTelemetryMiddleware(metrics,
		router.WithSkipPaths("/health", "/metrics", "/ping", "/docs"),
	))
	v0health.RegisterHealthEndpoint(api, "/v0", cfg, metrics)
	v0servers.RegisterServersEndpoints(api, "/v0", serverService, deploymentService)

	// Add /metrics for Prometheus metrics using promhttp
	mux.Handle("/metrics", metrics.PrometheusHandler())

	// Create request - using latest version endpoint
	url := "/v0/servers/" + url.PathEscape(server.Server.Name) + "/versions/latest"
	req := httptest.NewRequest(http.MethodGet, url, nil)
	w := httptest.NewRecorder()

	// Serve the request
	mux.ServeHTTP(w, req)

	// Check the status code
	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/metrics", nil)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	// shutdown metrics provider
	_ = shutdownTelemetry(context.Background())

	assert.Equal(t, http.StatusOK, w.Code, "Expected status OK for /metrics endpoint")

	body := w.Body.String()
	t.Log("metrics:", body)
	// Check if the response body contains expected metrics
	assert.Contains(t, body, "agent_registry_http_request_duration_bucket")
	assert.Contains(t, body, "agent_registry_http_requests_total")
	assert.Contains(t, body, "path=\"/v0/servers/{serverName}/versions/{version}\"")
}

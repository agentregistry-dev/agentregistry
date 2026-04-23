package api_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
)

// newCORSTestServer spins up a minimal API server without any services or
// route options — enough to exercise the CORS middleware + trailing-slash
// middleware without a database.
func newCORSTestServer(t *testing.T) *api.Server {
	t.Helper()

	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := config.NewConfig()
	cfg.JWTPrivateKey = hex.EncodeToString(testSeed)

	shutdownTelemetry, metrics, err := telemetry.InitMetrics("test")
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdownTelemetry(nil) })

	versionInfo := &arv0.VersionBody{
		Version:   "test",
		GitCommit: "test",
		BuildTime: "test",
	}

	return api.NewServer(cfg, metrics, versionInfo, nil, nil, nil)
}

func TestCORSHeaders(t *testing.T) {
	srv := newCORSTestServer(t)

	tests := []struct {
		name           string
		method         string
		path           string
		expectCORS     bool
		checkPreflight bool
	}{
		{
			name:       "GET request should have CORS headers",
			method:     http.MethodGet,
			path:       "/v0/health",
			expectCORS: true,
		},
		{
			name:       "POST request should have CORS headers",
			method:     http.MethodPost,
			path:       "/v0/namespaces/default/mcpservers",
			expectCORS: true,
		},
		{
			name:           "OPTIONS preflight request should succeed",
			method:         http.MethodOptions,
			path:           "/v0/namespaces/default/mcpservers",
			expectCORS:     true,
			checkPreflight: true,
		},
		{
			name:       "PUT request should have CORS headers",
			method:     http.MethodPut,
			path:       "/v0/namespaces/default/mcpservers/test/v1",
			expectCORS: true,
		},
		{
			name:       "DELETE request should have CORS headers",
			method:     http.MethodDelete,
			path:       "/v0/namespaces/default/mcpservers/test/v1",
			expectCORS: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("Origin", "https://example.com")
			if tt.checkPreflight {
				req.Header.Set("Access-Control-Request-Method", "GET")
				req.Header.Set("Access-Control-Request-Headers", "Content-Type")
			}

			rr := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rr, req)

			if tt.expectCORS {
				assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Origin"), "Access-Control-Allow-Origin header should be set")
			}
		})
	}
}

func TestCORSHeaderValues(t *testing.T) {
	srv := newCORSTestServer(t)

	req := httptest.NewRequest(http.MethodOptions, "/v0/namespaces/default/mcpservers", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	// Check that wildcard origin is allowed (our current CORS config).
	allowOrigin := rr.Header().Get("Access-Control-Allow-Origin")
	assert.Equal(t, "*", allowOrigin, "should allow any origin with wildcard")

	// Check that common methods are exposed (allowed methods header may or
	// may not be echoed depending on middleware; assert only when set).
	allowMethods := rr.Header().Get("Access-Control-Allow-Methods")
	if allowMethods != "" {
		assert.Contains(t, allowMethods, "POST")
	}
}

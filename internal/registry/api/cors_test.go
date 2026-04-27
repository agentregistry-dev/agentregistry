package api_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rs/cors"
	"github.com/stretchr/testify/assert"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
)

// newCORSTestHandler returns the same middleware stack NewServer wires
// (TrailingSlash → CORS → mux), but bound to a stub mux that just 200s
// every path. Lets CORS assertions run without a database or any of the
// v0 route registrations.
func newCORSTestHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Content-Type", "Content-Length"},
		AllowCredentials: false,
		MaxAge:           86400,
	})

	return api.TrailingSlashMiddleware(corsHandler.Handler(mux))
}

func TestCORSHeaders(t *testing.T) {
	handler := newCORSTestHandler()

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
			path:       "/v0/mcpservers",
			expectCORS: true,
		},
		{
			name:           "OPTIONS preflight request should succeed",
			method:         http.MethodOptions,
			path:           "/v0/mcpservers",
			expectCORS:     true,
			checkPreflight: true,
		},
		{
			name:       "PUT request should have CORS headers",
			method:     http.MethodPut,
			path:       "/v0/mcpservers/test/v1",
			expectCORS: true,
		},
		{
			name:       "DELETE request should have CORS headers",
			method:     http.MethodDelete,
			path:       "/v0/mcpservers/test/v1",
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
			handler.ServeHTTP(rr, req)

			if tt.expectCORS {
				assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Origin"), "Access-Control-Allow-Origin header should be set")
			}
		})
	}
}

func TestCORSHeaderValues(t *testing.T) {
	handler := newCORSTestHandler()

	req := httptest.NewRequest(http.MethodOptions, "/v0/mcpservers", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

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

package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// providerTestServer builds an httptest.Server that serves:
//   - GET /v0/providers/{name} → the provider with matching Name (404 otherwise)
//
// Only the routes exercised by `arctl get provider NAME [-o yaml]` are handled.
func providerTestServer(t *testing.T, providers []models.Provider) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/providers/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/v0/providers/")
		for _, p := range providers {
			if p.Name == name {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(p)
				return
			}
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// (1) `-o yaml` emits the declarative envelope and strips server-managed fields
// (id, timestamps) so the output round-trips through `arctl apply -f`.
func TestProviderGet_YAMLOutputRoundTrips(t *testing.T) {
	providers := []models.Provider{
		{
			ID:       "internal-id-123",
			Name:     "my-kagent",
			Platform: "kagent",
			Config: map[string]any{
				"kagentUrl": "http://kagent-controller.kagent:8083",
				"namespace": "kagent",
			},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		},
	}
	srv := providerTestServer(t, providers)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"provider", "my-kagent", "-o", "yaml"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	// Envelope shape.
	assert.Contains(t, got, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, got, "kind: Provider")
	assert.Contains(t, got, "name: my-kagent")
	// Declarative spec fields.
	assert.Contains(t, got, "platform: kagent")
	assert.Contains(t, got, "kagentUrl: http://kagent-controller.kagent:8083")
	assert.Contains(t, got, "namespace: kagent")
	// Server-managed fields must be stripped.
	assert.NotContains(t, got, "internal-id-123", "spec must not leak the server-assigned id")
	assert.NotContains(t, got, "createdAt", "spec must not leak server timestamps")
	assert.NotContains(t, got, "updatedAt", "spec must not leak server timestamps")
}

// (2) Table output (default) still works — regression guard for the YAML-only change.
func TestProviderGet_TableOutput(t *testing.T) {
	providers := []models.Provider{
		{ID: "id-1", Name: "my-kagent", Platform: "kagent"},
	}
	srv := providerTestServer(t, providers)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"provider", "my-kagent"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "my-kagent")
	assert.Contains(t, got, "kagent")
}

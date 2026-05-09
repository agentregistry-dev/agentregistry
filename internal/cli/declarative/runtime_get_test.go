package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// runtimeTestServer builds an httptest.Server that serves:
//   - GET /v0/runtimes/{name} → the runtime with matching Name (404 otherwise)
//
// Only the routes exercised by `arctl get runtime NAME [-o yaml]` are handled.
func runtimeTestServer(t *testing.T, runtimes []v1alpha1.Runtime) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/runtimes/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		name := strings.TrimPrefix(r.URL.Path, "/v0/runtimes/")
		for _, rt := range runtimes {
			if rt.Metadata.Name == name {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(rt)
				return
			}
		}
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func runtimeFixture(name, runtimeType string, config map[string]any) v1alpha1.Runtime {
	return v1alpha1.Runtime{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.GroupVersion,
			Kind:       v1alpha1.KindRuntime,
		},
		Metadata: v1alpha1.ObjectMeta{
			Namespace: v1alpha1.DefaultNamespace,
			Name:      name,
		},
		Spec: v1alpha1.RuntimeSpec{
			Type:   runtimeType,
			Config: config,
		},
	}
}

// (1) `-o yaml` emits the declarative envelope and strips server-managed fields
// (id, timestamps) so the output round-trips through `arctl apply -f`.
func TestRuntimeGet_YAMLOutputRoundTrips(t *testing.T) {
	runtimes := []v1alpha1.Runtime{
		runtimeFixture("my-kagent", "Kagent", map[string]any{
			"kagentUrl": "http://kagent-controller.kagent:8083",
			"namespace": "kagent",
		}),
	}
	srv := runtimeTestServer(t, runtimes)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"runtime", "my-kagent", "-o", "yaml"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	// Envelope shape.
	assert.Contains(t, got, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, got, "kind: Runtime")
	assert.Contains(t, got, "name: my-kagent")
	// Declarative spec fields.
	assert.Contains(t, got, "type: Kagent")
	assert.Contains(t, got, "kagentUrl: http://kagent-controller.kagent:8083")
	assert.Contains(t, got, "namespace: kagent")
	// Server-managed fields must be stripped.
	assert.NotContains(t, got, "createdAt", "spec must not leak server timestamps")
	assert.NotContains(t, got, "updatedAt", "spec must not leak server timestamps")
}

// (2) Table output (default) still works — regression guard for the YAML-only change.
func TestRuntimeGet_TableOutput(t *testing.T) {
	runtimes := []v1alpha1.Runtime{
		runtimeFixture("my-kagent", "Kagent", nil),
	}
	srv := runtimeTestServer(t, runtimes)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"runtime", "my-kagent"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "my-kagent")
	assert.Contains(t, got, "Kagent")
}

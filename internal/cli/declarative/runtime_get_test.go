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
		namespace := r.URL.Query().Get("namespace")
		if namespace == "" {
			namespace = v1alpha1.DefaultNamespace
		}
		for _, rt := range runtimes {
			if rt.Metadata.Name == name && rt.Metadata.NamespaceOrDefault() == namespace {
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

// (1) `-o yaml` emits the canonical v1alpha1 envelope without projecting
// metadata or spec through a CLI-specific DTO.
func TestRuntimeGet_YAMLOutputPreservesCanonicalEnvelope(t *testing.T) {
	runtimes := []v1alpha1.Runtime{
		runtimeFixture("my-kagent", "Kagent", map[string]any{
			"kagentUrl": "http://kagent-controller.kagent:8083",
			"namespace": "kagent",
		}),
	}
	runtimes[0].Metadata.Annotations = map[string]string{
		"reconcile.agentregistry.dev/force": "2026-06-16T12:00:00Z",
	}
	srv := runtimeTestServer(t, runtimes)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetOut(out)
	cmd.SetArgs([]string{"runtime", "my-kagent", "-o", "yaml"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	// Envelope shape.
	assert.Contains(t, got, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, got, "kind: Runtime")
	assert.Contains(t, got, "name: my-kagent")
	assert.Contains(t, got, "annotations:")
	assert.Contains(t, got, "reconcile.agentregistry.dev/force: \"2026-06-16T12:00:00Z\"")
	// Declarative spec fields.
	assert.Contains(t, got, "type: Kagent")
	assert.Contains(t, got, "kagentUrl: http://kagent-controller.kagent:8083")
	assert.Contains(t, got, "namespace: kagent")
}

// (2) Table output (default) still works — regression guard for the YAML-only change.
func TestRuntimeGet_TableOutput(t *testing.T) {
	runtimes := []v1alpha1.Runtime{
		runtimeFixture("my-kagent", "Kagent", nil),
	}
	srv := runtimeTestServer(t, runtimes)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetOut(out)
	cmd.SetArgs([]string{"runtime", "my-kagent"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "my-kagent")
	assert.Contains(t, got, "Kagent")
}

func TestRuntimeGet_ReturnsMatchByNamespaceName(t *testing.T) {
	defaultRuntime := runtimeFixture("my-kagent", "Local", nil)
	teamRuntime := runtimeFixture("my-kagent", "Kagent", map[string]any{"namespace": "team-kagent"})
	teamRuntime.Metadata.Namespace = "team-a"
	runtimes := []v1alpha1.Runtime{defaultRuntime, teamRuntime}
	srv := runtimeTestServer(t, runtimes)
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewGetCmd(declarativeTestDeps(nil))
	cmd.SetOut(out)
	cmd.SetArgs([]string{"runtime", "team-a/my-kagent"})
	require.NoError(t, cmd.Execute())

	got := out.String()
	assert.Contains(t, got, "my-kagent")
	assert.Contains(t, got, "Kagent")
	assert.NotContains(t, got, "Local")
}

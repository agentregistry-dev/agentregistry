package declarative_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// deploymentTestServer builds an httptest.Server routing:
//   - GET    /v0/deployments                → returns `list`
//   - DELETE /v0/deployments/{name}         → status 204 unless id is in `failIDs`, then 500
//
// Captures every received DELETE id in order for assertions.
func deploymentTestServer(t *testing.T, list []v1alpha1.Deployment, failIDs map[string]bool) (*httptest.Server, *[]string) {
	t.Helper()
	var mu sync.Mutex
	deleted := []string{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"items": list})
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/v0/deployments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/v0/deployments/")
		parts := strings.Split(path, "/")
		if len(parts) != 1 {
			http.Error(w, `{"error":"bad delete path"}`, http.StatusBadRequest)
			return
		}
		id := v1alpha1.DefaultNamespace + "/" + parts[0]
		mu.Lock()
		deleted = append(deleted, id)
		mu.Unlock()
		if failIDs[id] {
			http.Error(w, fmt.Sprintf(`{"error":"simulated delete failure for %s"}`, id), http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &deleted
}

func setupClientForServer(t *testing.T, srv *httptest.Server) {
	t.Helper()
	c := client.NewClient(srv.URL, "")
	declarative.SetAPIClient(c)
	t.Cleanup(func() { declarative.SetAPIClient(nil) })
}

// (1) Target-name delete fans out across every runtime variant AND every tag
// for that target — deployments don't carry a tag of their own, so the CLI
// can't (and shouldn't) narrow the cut by target tag here. Unrelated targets
// are left alone.
func TestDeploymentDelete_RemovesAllMatchesByTargetName(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("gcp-v1", "summarizer", "1.0.0", "my-gcp", "agent", "pending"),
		deploymentFixture("aws-v2", "summarizer", "2.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "pending"),
	}
	srv, deleted := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer"})
	require.NoError(t, cmd.Execute())

	assert.ElementsMatch(t, []string{"default/aws-v1", "default/gcp-v1", "default/aws-v2"}, *deleted,
		"every deployment targeting summarizer should be deleted; unrelated targets untouched")
}

// (2) When no deployment matches the target name, returns a not-found error.
func TestDeploymentDelete_NotFound(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v2", "other-target", "2.0.0", "my-aws", "agent", "pending"),
	}
	srv, deleted := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found",
		"no match should surface the registry not-found sentinel")
	assert.Empty(t, *deleted, "no DELETE requests should be issued when nothing matches")
}

// (3) When the server rejects one of the matching deletes, the error is surfaced and
// identifies the failing deployment — not silently ignored.
func TestDeploymentDelete_PartialFailure(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("gcp-v1", "summarizer", "1.0.0", "my-gcp", "agent", "pending"),
	}
	// Fail the GCP delete only.
	srv, deleted := deploymentTestServer(t, deployments, map[string]bool{"default/gcp-v1": true})
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer"})
	err := cmd.Execute()
	require.Error(t, err, "partial failure must propagate")
	assert.Contains(t, err.Error(), "default/gcp-v1", "error should identify which deployment failed")

	// Both DELETEs should have been attempted — we don't stop on first failure.
	assert.ElementsMatch(t, []string{"default/aws-v1", "default/gcp-v1"}, *deleted,
		"both matching deployments should be attempted even when one fails")
}

// (4) --tag is rejected for deployments and runtimes: neither kind has a tag
// of its own, so accepting one would let users confuse the target's tag (or
// nothing at all, for runtime) with the resource's identity.
func TestDelete_RejectsTagForDeploymentAndRuntime(t *testing.T) {
	for _, kind := range []string{"deployment", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			cmd := declarative.NewDeleteCmd()
			cmd.SetArgs([]string{kind, "anything", "--tag", "1.0.0"})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--tag is not supported for "+kind)
		})
	}
}

// (5) --force sends ?force=true query param to the server.
func TestDeploymentDelete_ForcePassesQueryParam(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
	}

	var capturedForce []string
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": deployments})
	})
	mux.HandleFunc("/v0/deployments/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedForce = append(capturedForce, r.URL.Query().Get("force"))
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer", "--force"})
	require.NoError(t, cmd.Execute())

	require.Len(t, capturedForce, 1)
	assert.Equal(t, "true", capturedForce[0], "?force=true must be sent when --force is passed")
}

// (6) --force cannot be combined with -f (file mode).
func TestDeploymentDelete_ForceWithFileReturnsError(t *testing.T) {
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"-f", "agent.yaml", "--force"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--force cannot be used with -f")
}

// (7) Without --force, no ?force query param is sent.
func TestDeploymentDelete_NoForceFlagOmitsQueryParam(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
	}

	var capturedQuery []string
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": deployments})
	})
	mux.HandleFunc("/v0/deployments/", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		capturedQuery = append(capturedQuery, r.URL.RawQuery)
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"deployment", "summarizer"})
	require.NoError(t, cmd.Execute())

	require.Len(t, capturedQuery, 1)
	assert.Empty(t, capturedQuery[0], "no query params should be sent without --force")
}

// (8) --force is rejected for non-deployment kinds.
func TestDelete_ForceRejectedForNonDeploymentKinds(t *testing.T) {
	for _, kind := range []string{"agent", "mcp", "skill", "prompt", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			cmd := declarative.NewDeleteCmd()
			cmd.SetArgs([]string{kind, "test-name", "--tag", "1.0.0", "--force"})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--force is only supported for deployments")
		})
	}
}

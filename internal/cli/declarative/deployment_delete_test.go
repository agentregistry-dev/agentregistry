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
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// deploymentTestServer builds an httptest.Server routing:
//   - GET    /v0/deployments                → returns `list`
//   - DELETE /v0/deployments/{name}         → status 204 unless id is in `failIDs`, then 500
//
// Captures every received DELETE id in order for assertions.
func deploymentTestServer(t *testing.T, list []v1alpha1.Deployment, failIDs map[string]bool) (*httptest.Server, *[]string, *[]string) {
	t.Helper()
	var mu sync.Mutex
	deleted := make([]string, 0)
	capturedQuery := make([]string, 0)
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
		path := strings.TrimPrefix(r.URL.Path, "/v0/deployments/")
		parts := strings.Split(path, "/")
		if len(parts) != 1 {
			http.Error(w, `{"error":"bad get/delete path"}`, http.StatusBadRequest)
			return
		}
		switch r.Method {
		case http.MethodGet:
			mu.Lock()
			defer mu.Unlock()
			for _, d := range list {
				if d.Metadata.Name == parts[0] {
					_ = json.NewEncoder(w).Encode(d)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		case http.MethodDelete:
			mu.Lock()
			capturedQuery = append(capturedQuery, r.URL.RawQuery)
			deleted = append(deleted, parts[0])
			mu.Unlock()
			if failIDs[parts[0]] {
				http.Error(w, fmt.Sprintf(`{"error":"simulated delete failure for %s"}`, parts[0]), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, &deleted, &capturedQuery
}

// (1) Target-name delete fans out across every runtime variant AND every tag
// for that target — deployments don't carry a tag of their own, so the CLI
// can't (and shouldn't) narrow the cut by target tag here. Unrelated targets
// are left alone.
func TestDeploymentDelete_RemovesNamedDeployment(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("gcp-v1", "summarizer", "1.0.0", "my-gcp", "agent", "pending"),
		deploymentFixture("aws-v2", "summarizer", "2.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "pending"),
	}
	srv, deleted, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"deployment", "aws-v1"})
	require.NoError(t, cmd.Execute())

	assert.ElementsMatch(t, []string{"aws-v1"}, *deleted,
		"every deployment targeting summarizer should be deleted; unrelated targets untouched")
}

// (2) When no deployment matches the target name, returns a not-found error.
func TestDeploymentDelete_NotFound(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v2", "other-target", "2.0.0", "my-aws", "agent", "pending"),
	}
	srv, deleted, _ := deploymentTestServer(t, deployments, nil)
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"deployment", "summarizer"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found",
		"no match should surface the registry not-found sentinel")
	assert.Empty(t, *deleted, "no DELETE requests should be issued when nothing matches")
}

func TestDeploymentDelete_ServerFailure(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "pending"),
		deploymentFixture("gcp-v1", "summarizer", "1.0.0", "my-gcp", "agent", "pending"),
	}
	// Fail the GCP delete only.
	srv, deleted, _ := deploymentTestServer(t, deployments, map[string]bool{"gcp-v1": true})
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"deployment", "gcp-v1"})
	err := cmd.Execute()
	require.Error(t, err, "failure must propagate")
	assert.Contains(t, err.Error(), "gcp-v1", "error should identify which deployment failed")

	// Both DELETEs should have been attempted — we don't stop on first failure.
	assert.ElementsMatch(t, []string{"gcp-v1"}, *deleted)
}

// (4) --tag is rejected for deployments and runtimes: neither kind has a tag
// of its own, so accepting one would let users confuse the target's tag (or
// nothing at all, for runtime) with the resource's identity.
func TestDelete_RejectsTagForDeploymentAndRuntime(t *testing.T) {
	for _, kind := range []string{"deployment", "runtime"} {
		t.Run(kind, func(t *testing.T) {
			cmd := declarative.NewDeleteCmd(declarativeTestDeps(nil))
			cmd.SetArgs([]string{kind, "anything", "--tag", "1.0.0"})
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "--tag is not supported for "+kind)
		})
	}
}

// (5) Deployment delete does not send legacy force query params. Teardown is
// controller/finalizer-owned.
func TestDeploymentDelete_OmitsLegacyForceQueryParam(t *testing.T) {
	deployments := []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
	}

	srv, _, capturedQuery := deploymentTestServer(t, deployments, map[string]bool{"gcp-v1": true})
	setupClientForServer(t, srv)

	cmd := declarative.NewDeleteCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"deployment", "aws-v1"})
	require.NoError(t, cmd.Execute())

	require.Len(t, *capturedQuery, 1)
	assert.Empty(t, (*capturedQuery)[0], "no query params should be sent")
}

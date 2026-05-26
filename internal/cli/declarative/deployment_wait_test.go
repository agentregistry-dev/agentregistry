package declarative_test

import (
	"bytes"
	"encoding/json"
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

// deploymentWaitTestServer serves /v0/deployments (list) and
// /v0/deployments/{name} (get) from the given list, matching by
// Metadata.Name.
func deploymentWaitTestServer(t *testing.T, deployments []v1alpha1.Deployment) *httptest.Server {
	t.Helper()
	var mu sync.Mutex
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": deployments})
	})
	mux.HandleFunc("/v0/deployments/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/v0/deployments/")
		parts := strings.Split(path, "/")
		if len(parts) != 1 {
			http.Error(w, `{"error":"bad get path"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		for _, d := range deployments {
			if d.Metadata.Name == parts[0] {
				_ = json.NewEncoder(w).Encode(d)
				return
			}
		}
		w.WriteHeader(http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// Already-deployed: wait returns immediately with the success line.
func TestDeploymentWait_DeployedReturnsImmediately(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "aws-v1", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/aws-v1 deployed")
}

// Terminal failure when waiting for "deployed" surfaces an error rather than
// waiting until the timeout. Error-message content is covered at the helper level.
func TestDeploymentWait_FailedSurfacesError(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "failed"),
	})
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"deployment", "aws-v1", "--timeout=1s"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `reached state "failed"`)
}

// --for=failed treats "failed" as the success condition.
func TestDeploymentWait_ForFailedSucceedsOnFailed(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "failed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "aws-v1", "--for=failed", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/aws-v1 failed")
}

// --for=delete against a registry with no matching deployment succeeds.
func TestDeploymentWait_ForDeleteSucceedsWhenAbsent(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "aws-v1", "--for=delete", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/aws-v1 deleted")
}

// A missing deployment name fails with "not found".
func TestDeploymentWait_NotFound(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"deployment", "aws-v1", "--timeout=1s"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// Unknown --for values are rejected up front instead of waiting until the timeout.
func TestDeploymentWait_RejectsUnknownForValue(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deploying"),
	})
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"deployment", "aws-v1", "--for=garbage", "--timeout=1s"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `invalid --for value "garbage"`)
}

// Non-deployment kinds are rejected.
func TestDeploymentWait_RejectsNonDeploymentKinds(t *testing.T) {
	srv := deploymentWaitTestServer(t, nil)
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"agent", "summarizer"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only supported for deployments")
}

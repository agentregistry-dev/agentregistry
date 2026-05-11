package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// deploymentWaitTestServer serves the /v0/deployments listing the wait
// command consumes. Returns a fresh server bound to t.Cleanup.
func deploymentWaitTestServer(t *testing.T, deployments []v1alpha1.Deployment) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v0/deployments", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": deployments})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

// (1) Already-deployed: wait returns immediately with the success line.
func TestDeploymentWait_DeployedReturnsImmediately(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/summarizer deployed")
}

// (2) Terminal failure when waiting for "deployed": surface the failure as an
// error, regardless of how long the helper would otherwise be willing to wait.
// (The Error-message surfacing path is covered by TestWaitForDeployment_TerminalFailureMismatch
// at the helper level — at the CLI level the DeploymentRecord.Error projection
// depends on which condition the server set, which the fixture deliberately
// keeps coarse.)
func TestDeploymentWait_FailedSurfacesError(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "failed"),
	})
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"deployment", "summarizer", "--interval=1ms", "--timeout=1s"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `reached state "failed"`)
}

// (3) `--for=failed` flips the semantics: failed is the success condition.
func TestDeploymentWait_ForFailedSucceedsOnFailed(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "failed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer", "--for=failed", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/summarizer failed")
}

// (4) `--for=delete` against a registry with no matching deployment is a
// success — the resource we were waiting on is gone.
func TestDeploymentWait_ForDeleteSucceedsWhenAbsent(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer", "--for=delete", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/summarizer deleted")
}

// (5) Wait targets the deployment's TARGET name, not its metadata name —
// matches how `get deployment` and `delete deployment` already address rows.
// A missing target name fails with "not found".
func TestDeploymentWait_NotFoundOnMissingTarget(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("other", "unrelated", "1.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	cmd := declarative.NewWaitCmd()
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"deployment", "summarizer", "--interval=1ms", "--timeout=1s"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// (6) --version restricts the wait to a specific target version. Two
// deployments share a target name; only the requested version is acted on.
func TestDeploymentWait_VersionFilter(t *testing.T) {
	srv := deploymentWaitTestServer(t, []v1alpha1.Deployment{
		deploymentFixture("aws-v1", "summarizer", "1.0.0", "my-aws", "agent", "deploying"),
		deploymentFixture("aws-v2", "summarizer", "2.0.0", "my-aws", "agent", "deployed"),
	})
	setupClientForServer(t, srv)

	out := &bytes.Buffer{}
	cmd := declarative.NewWaitCmd()
	cmd.SetOut(out)
	cmd.SetArgs([]string{"deployment", "summarizer", "--version=2.0.0", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute(),
		"wait must pick the matching version even when an older version is still deploying")
	assert.Contains(t, out.String(), "deployment/summarizer deployed")
}

// (7) Non-deployment kinds are rejected — wait only supports deployments today.
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

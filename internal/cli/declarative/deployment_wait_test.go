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

// deploymentWaitTestServer returns an httptest server serving /v0/deployments
// from the given list.
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

// Already-deployed: wait returns immediately with the success line.
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
	cmd.SetArgs([]string{"deployment", "summarizer", "--interval=1ms", "--timeout=1s"})

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
	cmd.SetArgs([]string{"deployment", "summarizer", "--for=failed", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/summarizer failed")
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
	cmd.SetArgs([]string{"deployment", "summarizer", "--for=delete", "--interval=1ms", "--timeout=1s"})

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "deployment/summarizer deleted")
}

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

// --version restricts the wait to one target version when two deployments
// share a target name.
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

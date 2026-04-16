package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agentYAML is a minimal valid Agent document used across apply tests.
const agentYAML = `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`

// batchApplyResponse builds a JSON body matching the POST /v0/apply response shape.
func batchApplyResponse(results []kinds.Result) []byte {
	body, _ := json.Marshal(map[string]any{"results": results})
	return body
}

// newApplyTestServer creates an httptest.Server that records the last request and
// replies with the provided batch response. Returns the server and a pointer to the
// captured request (populated after the first HTTP call).
func newApplyTestServer(t *testing.T, results []kinds.Result) (*httptest.Server, *http.Request) {
	t.Helper()
	captured := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = *r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(batchApplyResponse(results))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// setupApplyClient wires a client pointing at srv into the declarative package.
func setupApplyClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	c := client.NewClient(srv.URL, "")
	declarative.SetAPIClient(c)
	t.Cleanup(func() { declarative.SetAPIClient(nil) })
}

// writeTempYAML writes content to a temp file and returns its path.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(p, []byte(content), 0644))
	return p
}

// TestApplyPostsToBatchEndpoint verifies that apply sends POST to /v0/apply.
func TestApplyPostsToBatchEndpoint(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, http.MethodPost, captured.Method)
	assert.Equal(t, "/v0/apply", captured.URL.Path)
}

// TestApplyPrintsPerResourceStatus verifies stdout contains per-resource lines.
func TestApplyPrintsPerResourceStatus(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "a", Version: "1.0", Status: kinds.StatusApplied},
		{Kind: "deployment", Name: "x", Status: kinds.StatusFailed, Error: "drift detected"},
	}
	srv, _ := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var out bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	err := cmd.Execute()
	// Expect non-zero because of the failed result.
	require.Error(t, err)

	output := out.String()
	assert.Contains(t, output, "✓ agent/a")
	assert.Contains(t, output, "✗ deployment/x")
}

// TestApplyForceFlagThreads verifies that --force sets ?force=true on the request.
func TestApplyForceFlagThreads(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML), "--force"})
	require.NoError(t, cmd.Execute())

	parsedQuery, err := url.ParseQuery(captured.URL.RawQuery)
	require.NoError(t, err)
	assert.Equal(t, "true", parsedQuery.Get("force"), "expected ?force=true in request URL")
}

// TestApplyReturnsErrorOnAnyFailure verifies a StatusFailed result causes non-zero exit.
func TestApplyReturnsErrorOnAnyFailure(t *testing.T) {
	results := []kinds.Result{
		{Kind: "skill", Name: "my-skill", Status: kinds.StatusFailed, Error: "internal error"},
	}
	srv, _ := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one or more resources failed")
}

// TestApplyDryRunFlag verifies --dry-run sets ?dryRun=true on the request.
func TestApplyDryRunFlag(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML), "--dry-run"})
	require.NoError(t, cmd.Execute())

	parsedQuery, err := url.ParseQuery(captured.URL.RawQuery)
	require.NoError(t, err)
	assert.Equal(t, "true", parsedQuery.Get("dryRun"), "expected ?dryRun=true in request URL")
}

// TestApplyNoForceFalseNoise verifies that omitting --force does NOT add ?force=false.
func TestApplyNoForceFalseNoise(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Status: kinds.StatusApplied},
	}
	srv, captured := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	require.NoError(t, cmd.Execute())

	assert.Empty(t, captured.URL.RawQuery, "expected no query params when force and dryRun are false")
}

// TestApplyRejectsUnknownKind verifies that an unknown kind fails before hitting the server.
func TestApplyRejectsUnknownKind(t *testing.T) {
	declarative.SetAPIClient(nil)
	defer declarative.SetAPIClient(nil)

	badYAML := `apiVersion: ar.dev/v1alpha1
kind: UnknownKind
metadata:
  name: acme/test
  version: "1.0.0"
spec:
  description: "test"
`
	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, badYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown resource type") ||
		strings.Contains(err.Error(), "UnknownKind"))
}

// TestApplyDryRunOutputAnnotated verifies that --dry-run output includes "(dry run)" suffix
// and reports the status returned by the server (created/configured).
func TestApplyDryRunOutputAnnotated(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusCreated},
		{Kind: "skill", Name: "my-skill", Version: "2.0.0", Status: kinds.StatusConfigured},
	}
	srv, _ := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var out bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML), "--dry-run"})
	require.NoError(t, cmd.Execute())

	output := out.String()
	assert.Contains(t, output, "agent/acme/bot")
	assert.Contains(t, output, "created")
	assert.Contains(t, output, "(dry run)")
	assert.Contains(t, output, "skill/my-skill")
	assert.Contains(t, output, "configured")
}

// TestApplyNoAPIClient verifies that a missing API client returns an error.
func TestApplyNoAPIClient(t *testing.T) {
	declarative.SetAPIClient(nil)
	defer declarative.SetAPIClient(nil)

	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API client not initialized")
}

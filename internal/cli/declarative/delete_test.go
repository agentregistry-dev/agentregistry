package declarative_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// batchDeleteResponse builds a JSON body matching the DELETE /v0/apply response shape.
func batchDeleteResponse(results []kinds.Result) []byte {
	body, _ := json.Marshal(map[string]any{"results": results})
	return body
}

// newDeleteTestServer creates an httptest.Server that records the last request and
// replies with the provided batch response.
func newDeleteTestServer(t *testing.T, results []kinds.Result) (*httptest.Server, *http.Request) {
	t.Helper()
	captured := &http.Request{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = *r
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(batchDeleteResponse(results))
	}))
	t.Cleanup(srv.Close)
	return srv, captured
}

// setupDeleteClient wires a client pointing at srv into the declarative package.
func setupDeleteClient(t *testing.T, srv *httptest.Server) {
	t.Helper()
	c := client.NewClient(srv.URL, "")
	declarative.SetAPIClient(c)
	t.Cleanup(func() { declarative.SetAPIClient(nil) })
}

// TestDeleteFileModeUsesDeleteApplyEndpoint verifies that -f sends DELETE to /v0/apply.
func TestDeleteFileModeUsesDeleteApplyEndpoint(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	var buf bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, http.MethodDelete, captured.Method)
	assert.Equal(t, "/v0/apply", captured.URL.Path)
}

// TestDeleteFileModeReportsResults verifies that per-resource results are printed.
func TestDeleteFileModeReportsResults(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, _ := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	var out bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, out.String(), "agent/acme/bot")
}

// TestDeleteFileModeFailedResultsReturnError verifies that a failed result causes non-zero exit.
func TestDeleteFileModeFailedResultsReturnError(t *testing.T) {
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Status: kinds.StatusFailed, Error: "not found"},
	}
	srv, _ := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one or more resources failed to delete")
}

// TestDeleteFileModeRejectsUnknownKind verifies that an unknown kind fails before sending.
func TestDeleteFileModeRejectsUnknownKind(t *testing.T) {
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
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, badYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UnknownKind")
}

// TestDeleteFileModeNoAPIClient verifies that a missing API client returns an error.
func TestDeleteFileModeNoAPIClient(t *testing.T) {
	declarative.SetAPIClient(nil)
	defer declarative.SetAPIClient(nil)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"-f", writeTempYAML(t, agentYAML)})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API client not initialized")
}

// TestDeleteExplicitModeWithoutVersion verifies that --version is optional
// (providers don't use versions; the server validates if needed).
func TestDeleteExplicitModeWithoutVersion(t *testing.T) {
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"provider", "my-aws"})
	err := cmd.Execute()
	// Fails because no API client is set, but NOT because of missing version.
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "version")
}

// TestDeleteExplicitModeRequiresTwoArgs verifies that explicit mode without two args errors.
func TestDeleteExplicitModeRequiresTwoArgs(t *testing.T) {
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TYPE and NAME")
}

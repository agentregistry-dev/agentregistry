package declarative_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric/noop"
)

// newTestServer spins up an in-process HTTP server backed by the given FakeRegistry
// and returns a configured client plus a cleanup function.
func newTestServer(t *testing.T, fake *servicetesting.FakeRegistry) (*client.Client, func()) {
	t.Helper()

	mux := http.NewServeMux()
	meter := noop.NewMeterProvider().Meter("declarative-integration-tests")
	metrics, err := telemetry.NewMetrics(meter)
	if err != nil {
		t.Fatalf("failed to initialize test metrics: %v", err)
	}

	versionInfo := &apitypes.VersionBody{
		Version:   "test-version",
		GitCommit: "test-commit",
		BuildTime: "2026-01-02T03:04:05Z",
	}
	cfg := &config.Config{
		JWTPrivateKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	svcs := router.RegistryServices{
		Agent:      fake,
		Server:     fake,
		Skill:      fake,
		Prompt:     fake,
		Provider:   fake,
		Deployment: fake,
	}
	router.NewHumaAPI(cfg, svcs, mux, metrics, versionInfo, nil, nil, nil)
	server := httptest.NewServer(mux)

	c := client.NewClient(server.URL+"/v0", "test-token")
	return c, server.Close
}

// writeYAML writes content to a temp file and returns its path.
func writeYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

// --- get integration tests ---

func TestGetIntegration_ListAgents(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	fake.ListAgentsFn = func(_ context.Context, _ *database.AgentFilter, _ string, _ int) ([]*models.AgentResponse, string, error) {
		return []*models.AgentResponse{
			{
				Agent: models.AgentJSON{
					AgentManifest: models.AgentManifest{
						Name:          "acme/planner",
						Description:   "Planning agent",
						Version:       "1.0.0",
						Framework:     "adk",
						Language:      "python",
						ModelProvider: "google",
						ModelName:     "gemini-2.0-flash",
					},
					Version: "1.0.0",
				},
			},
		}, "", nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"agents"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "acme/planner")
}

func TestGetIntegration_GetAgent_YAML(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	fake.GetAgentByNameFn = func(_ context.Context, _ string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:          "acme/bot",
					Description:   "A test bot",
					Version:       "1.0.0",
					Framework:     "adk",
					Language:      "python",
					ModelProvider: "google",
					ModelName:     "gemini-2.0-flash",
				},
				Version: "1.0.0",
			},
		}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"agent", "acme/bot", "-o", "yaml"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "apiVersion: ar.dev/v1alpha1")
	assert.Contains(t, out, "kind: Agent")
	assert.Contains(t, out, "name: acme/bot")
	assert.Contains(t, out, "version: 1.0.0")
}

func TestGetIntegration_GetAgent_JSON(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	fake.GetAgentByNameFn = func(_ context.Context, _ string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:          "acme/bot",
					Description:   "A test bot",
					Version:       "1.0.0",
					Framework:     "adk",
					Language:      "python",
					ModelProvider: "google",
					ModelName:     "gemini-2.0-flash",
				},
				Version: "1.0.0",
			},
		}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"agent", "acme/bot", "-o", "json"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, `"name"`)
	assert.Contains(t, out, `"acme/bot"`)
	assert.Contains(t, out, `"framework"`)
}

// --- delete integration tests ---

func TestDeleteIntegration_Agent(t *testing.T) {
	var deletedName, deletedVersion string
	fake := servicetesting.NewFakeRegistry()
	fake.DeleteAgentFn = func(_ context.Context, name, version string) error {
		deletedName = name
		deletedVersion = version
		return nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent", "acme/bot", "--version", "1.0.0"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "acme/bot", deletedName)
	assert.Equal(t, "1.0.0", deletedVersion)
}

func TestDeleteIntegration_MCPServer(t *testing.T) {
	var deletedName, deletedVersion string
	fake := servicetesting.NewFakeRegistry()
	fake.DeleteServerFn = func(_ context.Context, name, version string) error {
		deletedName = name
		deletedVersion = version
		return nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"mcp", "acme/weather", "--version", "2.0.0"})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, "acme/weather", deletedName)
	assert.Equal(t, "2.0.0", deletedVersion)
}

func TestDeleteIntegration_FromFile(t *testing.T) {
	// File-mode delete sends DELETE /v0/apply with the YAML body.
	// Use an httptest server that handles the batch endpoint.
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	// Write a declarative YAML file.
	tmpDir := t.TempDir()
	yamlFile := filepath.Join(tmpDir, "agent.yaml")
	require.NoError(t, os.WriteFile(yamlFile, []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: 1.0.0
spec:
  image: localhost:5001/bot:latest
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
  description: test
`), 0o644))

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"-f", yamlFile})
	require.NoError(t, cmd.Execute())

	// Verify the request used DELETE /v0/apply.
	assert.Equal(t, http.MethodDelete, captured.Method)
	assert.Equal(t, "/v0/apply", captured.URL.Path)
}

func TestGetIntegration_ListMCPServers(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	fake.ListServersFn = func(_ context.Context, _ *database.ServerFilter, _ string, _ int) ([]*apiv0.ServerResponse, string, error) {
		return []*apiv0.ServerResponse{
			{
				Server: apiv0.ServerJSON{
					Name:        "acme/weather",
					Description: "Weather MCP server",
					Version:     "1.0.0",
				},
			},
		}, "", nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"mcps"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "acme/weather")
}

func TestGetIntegration_EmptyList(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	// f.Agents is nil/empty → returns empty list

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"agents"})
	require.NoError(t, cmd.Execute())

	assert.True(t, strings.Contains(buf.String(), "No agents found") ||
		buf.String() == "", "expected empty output or 'No agents found'")
}

func TestGetIntegration_GetAll(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	fake.ListAgentsFn = func(_ context.Context, _ *database.AgentFilter, _ string, _ int) ([]*models.AgentResponse, string, error) {
		return []*models.AgentResponse{
			{Agent: models.AgentJSON{AgentManifest: models.AgentManifest{Name: "summarizer", Version: "1.0.0"}, Version: "1.0.0"}},
		}, "", nil
	}
	fake.ListServersFn = func(_ context.Context, _ *database.ServerFilter, _ string, _ int) ([]*apiv0.ServerResponse, string, error) {
		return []*apiv0.ServerResponse{
			{Server: apiv0.ServerJSON{Name: "acme/fetch", Version: "1.0.0"}},
		}, "", nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"all"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	assert.Contains(t, out, "agents")
	assert.Contains(t, out, "summarizer")
	assert.Contains(t, out, "mcps")
	assert.Contains(t, out, "acme/fetch")
}

func TestDeleteIntegration_MissingVersion(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	// Version is optional at the CLI level (providers don't use it).
	// For agents, the server returns a 404 when version is empty.
	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent", "acme/bot"}) // no --version
	err := cmd.Execute()
	require.Error(t, err)
	// Error comes from the server (404), not from CLI version validation.
	assert.NotContains(t, err.Error(), "required flag")
}

func TestDeleteIntegration_WrongArgCount(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent"}) // only TYPE, no NAME
	err := cmd.Execute()
	require.Error(t, err)
}

func TestDeleteIntegration_FromFile_ServerReportsFailure(t *testing.T) {
	// File-mode delete: server-reported failures are printed and cause non-zero exit.
	// YAML without version is valid on the wire — the server decides how to handle it.
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bot", Status: kinds.StatusFailed, Error: "version required"},
	}
	srv, _ := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
spec:
  image: localhost:5001/bot:latest
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
  description: test
`)

	var outBuf bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)
	cmd.SetArgs([]string{"-f", yamlPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "one or more resources failed to delete")
}

func TestDeleteIntegration_FromFile_MultiDocBatchDelete(t *testing.T) {
	// Multi-doc YAML: all resources are sent as one DELETE /v0/apply batch.
	// The server reports per-resource results; partial failure exits non-zero.
	results := []kinds.Result{
		{Kind: "agent", Name: "acme/bad", Status: kinds.StatusFailed, Error: "not found"},
		{Kind: "agent", Name: "acme/good", Version: "1.0.0", Status: kinds.StatusApplied},
	}
	srv, captured := newDeleteTestServer(t, results)
	setupDeleteClient(t, srv)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bad
spec:
  description: missing version
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/good
  version: "1.0.0"
spec:
  image: localhost:5001/good:latest
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
  description: test
`)

	var outBuf bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetOut(&outBuf)
	cmd.SetErr(&outBuf)
	cmd.SetArgs([]string{"-f", yamlPath})
	err := cmd.Execute()
	require.Error(t, err, "should report error for failed resource")

	// Both resources reported — batch send confirmed.
	assert.Equal(t, http.MethodDelete, captured.Method)
	assert.Equal(t, "/v0/apply", captured.URL.Path)
	out := outBuf.String()
	assert.Contains(t, out, "acme/bad")
	assert.Contains(t, out, "acme/good")
}

func TestGetIntegration_GetAll_Empty(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	// everything empty

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	var buf bytes.Buffer
	cmd := declarative.NewGetCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"all"})
	require.NoError(t, cmd.Execute())

	assert.Contains(t, buf.String(), "No resources found.")
}

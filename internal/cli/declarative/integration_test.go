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

// --- apply integration tests ---

func TestApplyIntegration_Agent(t *testing.T) {
	var capturedReq *models.AgentJSON
	fake := servicetesting.NewFakeRegistry()
	fake.ApplyAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		capturedReq = req
		return &models.AgentResponse{Agent: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A test bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, capturedReq, "ApplyAgent was not called")
	assert.Equal(t, "acme/bot", capturedReq.Name)
	assert.Equal(t, "1.0.0", capturedReq.Version)
	assert.Equal(t, "adk", capturedReq.Framework)
	assert.Equal(t, "python", capturedReq.Language)
	assert.Equal(t, "google", capturedReq.ModelProvider)
	assert.Equal(t, "gemini-2.0-flash", capturedReq.ModelName)
	assert.Contains(t, buf.String(), "agent/acme/bot applied")
}

func TestApplyIntegration_MCPServer(t *testing.T) {
	var capturedReq *apiv0.ServerJSON
	fake := servicetesting.NewFakeRegistry()
	fake.CreateServerFn = func(_ context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
		capturedReq = req
		return &apiv0.ServerResponse{Server: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme/weather
  version: "1.0.0"
spec:
  description: "Weather MCP server"
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, capturedReq, "CreateServer was not called")
	assert.Equal(t, "acme/weather", capturedReq.Name)
	assert.Equal(t, "1.0.0", capturedReq.Version)
	assert.Contains(t, buf.String(), "mcpserver/acme/weather applied")
}

func TestApplyIntegration_MultiDoc(t *testing.T) {
	var agentCalled, serverCalled bool
	fake := servicetesting.NewFakeRegistry()
	fake.ApplyAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		agentCalled = true
		return &models.AgentResponse{Agent: *req}, nil
	}
	fake.CreateServerFn = func(_ context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
		serverCalled = true
		return &apiv0.ServerResponse{Server: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: acme/weather
  version: "1.0.0"
spec:
  description: "Weather MCP server"
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A test bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	assert.True(t, serverCalled, "CreateServer was not called")
	assert.True(t, agentCalled, "CreateAgent was not called")
	out := buf.String()
	assert.Contains(t, out, "mcpserver/acme/weather applied")
	assert.Contains(t, out, "agent/acme/bot applied")
}

func TestApplyIntegration_Skill(t *testing.T) {
	var capturedReq *models.SkillJSON
	fake := servicetesting.NewFakeRegistry()
	fake.ApplySkillFn = func(_ context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
		capturedReq = req
		return &models.SkillResponse{Skill: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: acme/summarize
  version: "1.0.0"
spec:
  description: "Summarizes text"
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, capturedReq, "ApplySkill was not called")
	assert.Equal(t, "acme/summarize", capturedReq.Name)
	assert.Equal(t, "1.0.0", capturedReq.Version)
	assert.Equal(t, "Summarizes text", capturedReq.Description)
	assert.Contains(t, buf.String(), "skill/acme/summarize applied")
}

func TestApplyIntegration_Prompt(t *testing.T) {
	var capturedReq *models.PromptJSON
	fake := servicetesting.NewFakeRegistry()
	fake.ApplyPromptFn = func(_ context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
		capturedReq = req
		return &models.PromptResponse{Prompt: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: acme/system
  version: "1.0.0"
spec:
  content: "You are a helpful assistant."
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	require.NotNil(t, capturedReq, "ApplyPrompt was not called")
	assert.Equal(t, "acme/system", capturedReq.Name)
	assert.Equal(t, "1.0.0", capturedReq.Version)
	assert.Equal(t, "You are a helpful assistant.", capturedReq.Content)
	assert.Contains(t, buf.String(), "prompt/acme/system applied")
}

func TestApplyIntegration_Idempotent_UpdatesExisting(t *testing.T) {
	// Verify that applying a resource that already exists succeeds via server-side upsert.
	// The ApplyAgent service method is called with the new data — no delete step.
	var applyCalls int
	var lastApplied *models.AgentJSON

	fake := servicetesting.NewFakeRegistry()
	fake.ApplyAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		applyCalls++
		lastApplied = req
		return &models.AgentResponse{Agent: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:v2
  description: "Updated bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	assert.Equal(t, 1, applyCalls, "ApplyAgent should be called exactly once")
	require.NotNil(t, lastApplied)
	assert.Equal(t, "acme/bot", lastApplied.Name)
	assert.Contains(t, buf.String(), "agent/acme/bot applied")
}

func TestApplyIntegration_CreatesWhenNotFound(t *testing.T) {
	// Verify that applying a resource that does not yet exist succeeds.
	// The upsert (ApplyAgent) path handles both create and update transparently.
	var applyCalled bool

	fake := servicetesting.NewFakeRegistry()
	fake.ApplyAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		applyCalled = true
		return &models.AgentResponse{Agent: *req}, nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	yamlPath := writeYAML(t, `
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/new-bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/new-bot:latest
  description: "New bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath})
	require.NoError(t, cmd.Execute())

	assert.True(t, applyCalled, "ApplyAgent was not called")
	assert.Contains(t, buf.String(), "agent/acme/new-bot applied")
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

	assert.Equal(t, "acme/bot", deletedName)
	assert.Equal(t, "1.0.0", deletedVersion)
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

	cmd := declarative.NewDeleteCmd()
	cmd.SetArgs([]string{"agent", "acme/bot"}) // no --version
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "version")
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

func TestDeleteIntegration_FromFile_MissingVersion(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

	// YAML with no metadata.version
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

	var errBuf bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"-f", yamlPath})
	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, errBuf.String(), "failed to delete")
}

func TestDeleteIntegration_FromFile_ContinuesOnError(t *testing.T) {
	// Multi-doc: first resource has no version, second should still be attempted.
	var deletedNames []string
	fake := servicetesting.NewFakeRegistry()
	fake.DeleteAgentFn = func(_ context.Context, name, version string) error {
		deletedNames = append(deletedNames, name)
		return nil
	}

	c, cleanup := newTestServer(t, fake)
	defer cleanup()
	declarative.SetAPIClient(c)

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

	var errBuf bytes.Buffer
	cmd := declarative.NewDeleteCmd()
	cmd.SetErr(&errBuf)
	cmd.SetArgs([]string{"-f", yamlPath})
	err := cmd.Execute()
	require.Error(t, err, "should report error for missing version")
	// The second resource (acme/good) must still be deleted
	assert.Contains(t, deletedNames, "acme/good", "second resource should be processed despite first error")
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

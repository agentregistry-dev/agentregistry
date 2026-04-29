package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstructImageName(t *testing.T) {
	// Save original DockerRegistry and restore after test
	originalRegistry := version.DockerRegistry
	defer func() { version.DockerRegistry = originalRegistry }()

	tests := []struct {
		name           string
		dockerRegistry string
		flagImage      string
		manifestImage  string
		agentName      string
		want           string
	}{
		{
			name:           "flag image takes priority",
			dockerRegistry: "localhost:5001",
			flagImage:      "ghcr.io/myorg/myagent:v1.0",
			manifestImage:  "docker.io/user/agent:latest",
			agentName:      "myagent",
			want:           "ghcr.io/myorg/myagent:v1.0",
		},
		{
			name:           "manifest image used when flag empty",
			dockerRegistry: "localhost:5001",
			flagImage:      "",
			manifestImage:  "docker.io/user/agent:v2.0",
			agentName:      "myagent",
			want:           "docker.io/user/agent:v2.0",
		},
		{
			name:           "default constructed when both empty",
			dockerRegistry: "localhost:5001",
			flagImage:      "",
			manifestImage:  "",
			agentName:      "myagent",
			want:           "localhost:5001/myagent:latest",
		},
		{
			name:           "uses custom docker registry from version",
			dockerRegistry: "gcr.io/myproject",
			flagImage:      "",
			manifestImage:  "",
			agentName:      "myagent",
			want:           "gcr.io/myproject/myagent:latest",
		},
		{
			name:           "docker registry with trailing slash is trimmed",
			dockerRegistry: "gcr.io/myproject/",
			flagImage:      "",
			manifestImage:  "",
			agentName:      "myagent",
			want:           "gcr.io/myproject/myagent:latest",
		},
		{
			name:           "empty docker registry falls back to localhost",
			dockerRegistry: "",
			flagImage:      "",
			manifestImage:  "",
			agentName:      "myagent",
			want:           "localhost:5001/myagent:latest",
		},
		{
			name:           "flag image with no tag",
			dockerRegistry: "localhost:5001",
			flagImage:      "myregistry.com/myimage",
			manifestImage:  "",
			agentName:      "myagent",
			want:           "myregistry.com/myimage",
		},
		{
			name:           "manifest image with digest",
			dockerRegistry: "localhost:5001",
			flagImage:      "",
			manifestImage:  "docker.io/user/agent@sha256:abc123",
			agentName:      "myagent",
			want:           "docker.io/user/agent@sha256:abc123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version.DockerRegistry = tt.dockerRegistry
			got := ConstructImageName(tt.flagImage, tt.manifestImage, tt.agentName)
			if got != tt.want {
				t.Errorf("ConstructImageName(%q, %q, %q) = %q, want %q",
					tt.flagImage, tt.manifestImage, tt.agentName, got, tt.want)
			}
		})
	}
}

func TestConstructMCPServerImageName(t *testing.T) {
	// Save original DockerRegistry and restore after test
	originalRegistry := version.DockerRegistry
	defer func() { version.DockerRegistry = originalRegistry }()

	tests := []struct {
		name           string
		dockerRegistry string
		agentName      string
		serverName     string
		want           string
	}{
		{
			name:           "normal case",
			dockerRegistry: "localhost:5001",
			agentName:      "myagent",
			serverName:     "weather",
			want:           "localhost:5001/myagent-weather:latest",
		},
		{
			name:           "empty agent name defaults to agent",
			dockerRegistry: "localhost:5001",
			agentName:      "",
			serverName:     "weather",
			want:           "localhost:5001/agent-weather:latest",
		},
		{
			name:           "uses custom docker registry",
			dockerRegistry: "ghcr.io/myorg",
			agentName:      "myagent",
			serverName:     "database",
			want:           "ghcr.io/myorg/myagent-database:latest",
		},
		{
			name:           "docker registry with trailing slash",
			dockerRegistry: "gcr.io/myproject/",
			agentName:      "myagent",
			serverName:     "cache",
			want:           "gcr.io/myproject/myagent-cache:latest",
		},
		{
			name:           "empty docker registry falls back to localhost",
			dockerRegistry: "",
			agentName:      "myagent",
			serverName:     "api",
			want:           "localhost:5001/myagent-api:latest",
		},
		{
			name:           "server name with hyphens",
			dockerRegistry: "localhost:5001",
			agentName:      "myagent",
			serverName:     "my-mcp-server",
			want:           "localhost:5001/myagent-my-mcp-server:latest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version.DockerRegistry = tt.dockerRegistry
			got := ConstructMCPServerImageName(tt.agentName, tt.serverName)
			if got != tt.want {
				t.Errorf("ConstructMCPServerImageName(%q, %q) = %q, want %q",
					tt.agentName, tt.serverName, got, tt.want)
			}
		})
	}
}

func TestEnsureOtelCollectorConfig(t *testing.T) {
	tests := []struct {
		name          string
		telemetry     string
		preCreate     bool
		wantFileExist bool
	}{
		{
			name:          "no telemetry endpoint - file not created",
			telemetry:     "",
			preCreate:     false,
			wantFileExist: false,
		},
		{
			name:          "telemetry set and file missing - file created",
			telemetry:     "http://localhost:4318/v1/traces",
			preCreate:     false,
			wantFileExist: true,
		},
		{
			name:          "telemetry set and file exists - file preserved",
			telemetry:     "http://localhost:4318/v1/traces",
			preCreate:     true,
			wantFileExist: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			agent := &v1alpha1.Agent{
				Metadata: v1alpha1.ObjectMeta{Name: "test-agent"},
				Spec:     v1alpha1.AgentSpec{TelemetryEndpoint: tt.telemetry},
			}

			configPath := filepath.Join(dir, "otel-collector-config.yaml")
			if tt.preCreate {
				if err := os.WriteFile(configPath, []byte("custom-config"), 0o644); err != nil {
					t.Fatalf("failed to pre-create config: %v", err)
				}
			}

			err := EnsureOtelCollectorConfig(dir, agent, false)
			if err != nil {
				t.Fatalf("EnsureOtelCollectorConfig() error = %v", err)
			}

			_, statErr := os.Stat(configPath)
			fileExists := statErr == nil

			if fileExists != tt.wantFileExist {
				t.Errorf("file exists = %v, want %v", fileExists, tt.wantFileExist)
			}

			// If file was pre-created, ensure it wasn't overwritten
			if tt.preCreate && fileExists {
				content, _ := os.ReadFile(configPath)
				if string(content) != "custom-config" {
					t.Errorf("pre-existing file was overwritten")
				}
			}
		})
	}
}

func TestLoadAgent_EnvelopeFormat(t *testing.T) {
	dir := t.TempDir()
	envelopeYAML := `apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: summarizer
  version: "1.0.0"
spec:
  image: ghcr.io/acme/summarizer:v1
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
  description: "Summarizes documents"
  telemetryEndpoint: "http://localhost:4318/v1/traces"
  mcpServers:
    - kind: MCPServer
      name: acme/fetch
      version: "1.0.0"
  skills:
    - kind: Skill
      name: acme/summarize
      version: "1.0.0"
  prompts:
    - kind: Prompt
      name: acme/system
      version: "1.0.0"
  repository:
    url: https://github.com/acme/summarizer
    source: github
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(envelopeYAML), 0o644))
	got, err := LoadAgent(dir)
	require.NoError(t, err)
	require.NotNil(t, got)

	// Envelope decode is offline; the runnable runtime form is built
	// later by manifest.Resolve (which makes registry calls). LoadAgent
	// only verifies the envelope round-trips into v1alpha1.Agent.
	assert.Equal(t, "summarizer", got.Metadata.Name)
	assert.Equal(t, "1.0.0", got.Metadata.Version)
	assert.Equal(t, "ghcr.io/acme/summarizer:v1", got.Spec.Image)
	assert.Equal(t, "python", got.Spec.Language)
	assert.Equal(t, "adk", got.Spec.Framework)
	assert.Equal(t, "gemini", got.Spec.ModelProvider)
	assert.Equal(t, "gemini-2.0-flash", got.Spec.ModelName)

	require.Len(t, got.Spec.MCPServers, 1)
	assert.Equal(t, "acme/fetch", got.Spec.MCPServers[0].Name)
	assert.Equal(t, "1.0.0", got.Spec.MCPServers[0].Version)

	require.Len(t, got.Spec.Skills, 1)
	assert.Equal(t, "acme/summarize", got.Spec.Skills[0].Name)
	assert.Equal(t, "1.0.0", got.Spec.Skills[0].Version)

	require.Len(t, got.Spec.Prompts, 1)
	assert.Equal(t, "acme/system", got.Spec.Prompts[0].Name)
	assert.Equal(t, "1.0.0", got.Spec.Prompts[0].Version)
}

// TestLoadAgent_RejectsLegacyFlatFormat pins the contract that the
// flat AgentManifest YAML shape (agentName / image / language at top
// level, no apiVersion) is no longer accepted on disk. The on-disk
// manifest must be a v1alpha1.Agent envelope; the legacy decoder was
// removed alongside the dual-format LoadManifest dispatch.
func TestLoadAgent_RejectsLegacyFlatFormat(t *testing.T) {
	dir := t.TempDir()
	flatYAML := `agentName: legacy
image: ghcr.io/acme/legacy:v1
language: python
framework: adk
modelProvider: gemini
modelName: gemini-2.0-flash
description: "Legacy flat manifest"
`
	require.NoError(t, os.WriteFile(filepath.Join(dir, "agent.yaml"), []byte(flatYAML), 0o644))
	_, err := LoadAgent(dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not a v1alpha1 envelope")
}

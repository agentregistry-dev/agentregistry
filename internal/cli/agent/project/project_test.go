package project

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
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
			manifest := &models.AgentManifest{
				Name:              "test-agent",
				TelemetryEndpoint: tt.telemetry,
			}

			configPath := filepath.Join(dir, "otel-collector-config.yaml")
			if tt.preCreate {
				if err := os.WriteFile(configPath, []byte("custom-config"), 0o644); err != nil {
					t.Fatalf("failed to pre-create config: %v", err)
				}
			}

			err := EnsureOtelCollectorConfig(dir, manifest, false)
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

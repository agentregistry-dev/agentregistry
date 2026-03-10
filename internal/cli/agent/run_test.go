package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestHasRegistryServers(t *testing.T) {
	tests := []struct {
		name     string
		manifest *models.AgentManifest
		want     bool
	}{
		{
			name: "no MCP servers",
			manifest: &models.AgentManifest{
				Name:       "test-agent",
				McpServers: nil,
			},
			want: false,
		},
		{
			name: "empty MCP servers list",
			manifest: &models.AgentManifest{
				Name:       "test-agent",
				McpServers: []models.McpServerType{},
			},
			want: false,
		},
		{
			name: "only command type servers",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "command", Name: "server1"},
					{Type: "command", Name: "server2"},
				},
			},
			want: false,
		},
		{
			name: "only remote type servers",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "remote", Name: "server1"},
				},
			},
			want: false,
		},
		{
			name: "has one registry server",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "registry", Name: "server1"},
				},
			},
			want: true,
		},
		{
			name: "mixed types with registry server",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "command", Name: "cmd-server"},
					{Type: "registry", Name: "reg-server"},
					{Type: "remote", Name: "remote-server"},
				},
			},
			want: true,
		},
		{
			name: "registry server in middle of list",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "command", Name: "server1"},
					{Type: "command", Name: "server2"},
					{Type: "registry", Name: "server3"},
					{Type: "command", Name: "server4"},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasRegistryServers(tt.manifest)
			if got != tt.want {
				t.Errorf("hasRegistryServers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveMCPServersForRuntime(t *testing.T) {
	tests := []struct {
		name         string
		manifest     *models.AgentManifest
		wantErr      bool
		wantResolved int
		wantConfig   int
	}{
		{
			name:     "nil manifest",
			manifest: nil,
			wantErr:  true,
		},
		{
			name: "no registry servers",
			manifest: &models.AgentManifest{
				Name: "test-agent",
				McpServers: []models.McpServerType{
					{Type: "command", Name: "cmd"},
					{Type: "remote", Name: "remote"},
				},
			},
			wantResolved: 0,
			wantConfig:   0,
		},
		{
			name: "empty mcp servers",
			manifest: &models.AgentManifest{
				Name:       "test-agent",
				McpServers: []models.McpServerType{},
			},
			wantResolved: 0,
			wantConfig:   0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolved, config, err := resolveMCPServersForRuntime(tt.manifest)
			if (err != nil) != tt.wantErr {
				t.Fatalf("resolveMCPServersForRuntime() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if len(resolved) != tt.wantResolved {
				t.Fatalf("resolveMCPServersForRuntime() resolved count = %d, want %d", len(resolved), tt.wantResolved)
			}
			if len(config) != tt.wantConfig {
				t.Fatalf("resolveMCPServersForRuntime() config count = %d, want %d", len(config), tt.wantConfig)
			}
		})
	}
}

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name          string
		modelProvider string
		osEnv         map[string]string
		extraEnv      map[string]string
		wantErr       bool
		errContain    string
	}{
		{
			name:          "openai with key set (os)",
			modelProvider: "openai",
			osEnv:         map[string]string{"OPENAI_API_KEY": "sk-test-key"},
			wantErr:       false,
		},
		{
			name:          "openai with key set (extra)",
			modelProvider: "openai",
			extraEnv:      map[string]string{"OPENAI_API_KEY": "sk-test-key"},
			wantErr:       false,
		},
		{
			name:          "openai without key",
			modelProvider: "openai",
			wantErr:       true,
			errContain:    "OPENAI_API_KEY",
		},
		{
			name:          "anthropic with key set (os)",
			modelProvider: "anthropic",
			osEnv:         map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"},
			wantErr:       false,
		},
		{
			name:          "anthropic with key set (extra)",
			modelProvider: "anthropic",
			extraEnv:      map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"},
			wantErr:       false,
		},
		{
			name:          "anthropic without key",
			modelProvider: "anthropic",
			wantErr:       true,
			errContain:    "ANTHROPIC_API_KEY",
		},
		{
			name:          "azureopenai with key set (os)",
			modelProvider: "azureopenai",
			osEnv:         map[string]string{"AZUREOPENAI_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "azureopenai with key set (extra)",
			modelProvider: "azureopenai",
			extraEnv:      map[string]string{"AZUREOPENAI_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "azureopenai without key",
			modelProvider: "azureopenai",
			wantErr:       true,
			errContain:    "AZUREOPENAI_API_KEY",
		},
		{
			name:          "gemini with key set (os)",
			modelProvider: "gemini",
			osEnv:         map[string]string{"GOOGLE_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "gemini with key set (extra)",
			modelProvider: "gemini",
			extraEnv:      map[string]string{"GOOGLE_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "unknown provider returns no error",
			modelProvider: "custom-llm",
			wantErr:       false,
		},
		{
			name:          "empty provider returns no error",
			modelProvider: "",
			wantErr:       false,
		},
		{
			name:          "case insensitive - OpenAI uppercase",
			modelProvider: "OpenAI",
			wantErr:       true,
			errContain:    "OPENAI_API_KEY",
		},
		{
			name:          "case insensitive - GEMINI uppercase",
			modelProvider: "GEMINI",
			wantErr:       true,
			errContain:    "GOOGLE_API_KEY",
		},
		{
			name:          "key in extra env only",
			modelProvider: "gemini",
			extraEnv:      map[string]string{"GOOGLE_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "key in os env only",
			modelProvider: "openai",
			osEnv:         map[string]string{"OPENAI_API_KEY": "sk-test"},
			wantErr:       false,
		},
		{
			name:          "key missing from both",
			modelProvider: "anthropic",
			wantErr:       true,
			errContain:    "ANTHROPIC_API_KEY",
		},
		{
			name:          "nil extra env falls back to os",
			modelProvider: "openai",
			osEnv:         map[string]string{"OPENAI_API_KEY": "sk-test"},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars before test
			for _, envVar := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AZUREOPENAI_API_KEY"} {
				os.Unsetenv(envVar)
			}

			// Set up env vars for this test
			for k, v := range tt.osEnv {
				os.Setenv(k, v)
			}

			// Clean up after test
			defer func() {
				for k := range tt.osEnv {
					os.Unsetenv(k)
				}
			}()

			err := validateAPIKey(tt.modelProvider, tt.extraEnv)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPIKey(%q) error = %v, wantErr %v",
					tt.modelProvider, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("validateAPIKey(%q) error = %v, want error containing %q",
						tt.modelProvider, err, tt.errContain)
				}
			}
		})
	}
}

func TestRenderComposeFromManifest_WithSkills(t *testing.T) {
	manifest := &models.AgentManifest{
		Name:          "test-agent",
		Image:         "docker.io/org/test-agent:latest",
		ModelProvider: "openai",
		ModelName:     "gpt-4o",
		Skills: []models.SkillRef{
			{Name: "skill-a", Image: "docker.io/org/skill-a:latest"},
		},
	}

	data, err := renderComposeFromManifest(manifest, "1.2.3")
	if err != nil {
		t.Fatalf("renderComposeFromManifest() error = %v", err)
	}

	rendered := string(data)
	if !strings.Contains(rendered, "KAGENT_SKILLS_FOLDER=/skills") {
		t.Fatalf("expected rendered compose to include KAGENT_SKILLS_FOLDER")
	}
	if !strings.Contains(rendered, "source: ./test-agent/1.2.3/skills") {
		t.Fatalf("expected rendered compose to include skills bind mount source path")
	}
	if !strings.Contains(rendered, "target: /skills") {
		t.Fatalf("expected rendered compose to include /skills mount target")
	}
}

func TestRenderComposeFromManifest_WithoutSkills(t *testing.T) {
	manifest := &models.AgentManifest{
		Name:          "test-agent",
		Image:         "docker.io/org/test-agent:latest",
		ModelProvider: "openai",
		ModelName:     "gpt-4o",
	}

	data, err := renderComposeFromManifest(manifest, "1.2.3")
	if err != nil {
		t.Fatalf("renderComposeFromManifest() error = %v", err)
	}

	rendered := string(data)
	if strings.Contains(rendered, "KAGENT_SKILLS_FOLDER=/skills") {
		t.Fatalf("expected rendered compose not to include KAGENT_SKILLS_FOLDER")
	}
	if strings.Contains(rendered, "source: ./test-agent/1.2.3/skills") {
		t.Fatalf("expected rendered compose not to include skills bind mount source path")
	}
}

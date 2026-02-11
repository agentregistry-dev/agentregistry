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

func TestValidateAPIKey(t *testing.T) {
	tests := []struct {
		name          string
		modelProvider string
		envSetup      map[string]string
		wantErr       bool
		errContain    string
	}{
		{
			name:          "openai with key set",
			modelProvider: "openai",
			envSetup:      map[string]string{"OPENAI_API_KEY": "sk-test-key"},
			wantErr:       false,
		},
		{
			name:          "openai without key",
			modelProvider: "openai",
			envSetup:      map[string]string{},
			wantErr:       true,
			errContain:    "OPENAI_API_KEY",
		},
		{
			name:          "anthropic with key set",
			modelProvider: "anthropic",
			envSetup:      map[string]string{"ANTHROPIC_API_KEY": "sk-ant-test"},
			wantErr:       false,
		},
		{
			name:          "anthropic without key",
			modelProvider: "anthropic",
			envSetup:      map[string]string{},
			wantErr:       true,
			errContain:    "ANTHROPIC_API_KEY",
		},
		{
			name:          "gemini with key set",
			modelProvider: "gemini",
			envSetup:      map[string]string{"GOOGLE_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "gemini without key",
			modelProvider: "gemini",
			envSetup:      map[string]string{},
			wantErr:       true,
			errContain:    "GOOGLE_API_KEY",
		},
		{
			name:          "azureopenai with key set",
			modelProvider: "azureopenai",
			envSetup:      map[string]string{"AZUREOPENAI_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "azureopenai without key",
			modelProvider: "azureopenai",
			envSetup:      map[string]string{},
			wantErr:       true,
			errContain:    "AZUREOPENAI_API_KEY",
		},
		{
			name:          "unknown provider returns no error",
			modelProvider: "unknown-provider",
			envSetup:      map[string]string{},
			wantErr:       false,
		},
		{
			name:          "empty provider returns no error",
			modelProvider: "",
			envSetup:      map[string]string{},
			wantErr:       false,
		},
		{
			name:          "case insensitive - OpenAI uppercase",
			modelProvider: "OpenAI",
			envSetup:      map[string]string{"OPENAI_API_KEY": "sk-test"},
			wantErr:       false,
		},
		{
			name:          "case insensitive - GEMINI uppercase without key",
			modelProvider: "GEMINI",
			envSetup:      map[string]string{},
			wantErr:       true,
			errContain:    "GOOGLE_API_KEY",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear relevant env vars before test
			for _, envVar := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AZUREOPENAI_API_KEY"} {
				os.Unsetenv(envVar)
			}

			// Set up env vars for this test
			for k, v := range tt.envSetup {
				os.Setenv(k, v)
			}

			// Clean up after test
			defer func() {
				for k := range tt.envSetup {
					os.Unsetenv(k)
				}
			}()

			err := validateAPIKey(tt.modelProvider)
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

package agent

import (
	"os"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestHasBuildableServers(t *testing.T) {
	tests := []struct {
		name string
		in   []agentmanifest.ResolvedMCPServer
		want bool
	}{
		{
			name: "nil",
			in:   nil,
			want: false,
		},
		{
			name: "command with OCI image (no build)",
			in: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "oci", Image: "ghcr.io/x/y:1"},
			},
			want: false,
		},
		{
			name: "remote only",
			in: []agentmanifest.ResolvedMCPServer{
				{Type: "remote", Name: "rem", URL: "https://example.com/mcp"},
			},
			want: false,
		},
		{
			name: "command with registry build",
			in: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "npm", Build: "registry/npm"},
			},
			want: true,
		},
		{
			name: "mixed: one buildable",
			in: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "oci", Image: "ghcr.io/x/y:1"},
				{Type: "command", Name: "npm", Build: "registry/npm"},
				{Type: "remote", Name: "rem", URL: "https://example.com/mcp"},
			},
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasBuildableServers(tt.in)
			if got != tt.want {
				t.Errorf("hasBuildableServers() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPythonServersFromResolved(t *testing.T) {
	tests := []struct {
		name string
		in   []agentmanifest.ResolvedMCPServer
		want []common.PythonMCPServer
	}{
		{
			name: "empty",
			in:   nil,
			want: nil,
		},
		{
			name: "remote + command",
			in: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "cmd"},
				{Type: "remote", Name: "rem", URL: "https://example.com", Headers: map[string]string{"x-api-key": "k"}},
			},
			want: []common.PythonMCPServer{
				{Name: "cmd", Type: "command"},
				{Name: "rem", Type: "remote", URL: "https://example.com", Headers: map[string]string{"x-api-key": "k"}},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pythonServersFromResolved(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("len = %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].Name != tt.want[i].Name || got[i].Type != tt.want[i].Type {
					t.Errorf("entry[%d] mismatch: got %+v want %+v", i, got[i], tt.want[i])
				}
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

func resolvedAgentForTest(name, image, modelProvider, modelName string, skills []v1alpha1.ResourceRef) *agentmanifest.ResolvedAgent {
	return &agentmanifest.ResolvedAgent{
		Agent: &v1alpha1.Agent{
			Metadata: v1alpha1.ObjectMeta{Name: name},
			Spec: v1alpha1.AgentSpec{
				Source:        &v1alpha1.AgentSource{Image: image},
				ModelProvider: modelProvider,
				ModelName:     modelName,
				Skills:        skills,
			},
		},
	}
}

func TestRenderComposeFromManifest_WithSkills(t *testing.T) {
	resolved := resolvedAgentForTest(
		"test-agent",
		"docker.io/org/test-agent:latest",
		"openai", "gpt-4o",
		[]v1alpha1.ResourceRef{{Kind: v1alpha1.KindSkill, Name: "skill-a", Version: "1"}},
	)

	data, err := renderComposeFromManifest(resolved, "1.2.3", 8080)
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
	resolved := resolvedAgentForTest("test-agent", "docker.io/org/test-agent:latest", "openai", "gpt-4o", nil)

	data, err := renderComposeFromManifest(resolved, "1.2.3", 8080)
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

func TestRenderComposeFromManifest_CustomPort(t *testing.T) {
	resolved := resolvedAgentForTest("test-agent", "docker.io/org/test-agent:latest", "openai", "gpt-4o", nil)

	data, err := renderComposeFromManifest(resolved, "1.2.3", 9876)
	if err != nil {
		t.Fatalf("renderComposeFromManifest() error = %v", err)
	}

	rendered := string(data)
	if !strings.Contains(rendered, "\"9876:8080\"") {
		t.Fatalf("expected rendered compose to map host port 9876 to container port 8080, got:\n%s", rendered)
	}
	if strings.Contains(rendered, "\"8080:8080\"") {
		t.Fatalf("expected rendered compose not to contain default 8080:8080 mapping")
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort() error = %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Fatalf("freePort() returned invalid port: %d", port)
	}
}

func TestFilterServersToBuild(t *testing.T) {
	tests := []struct {
		name    string
		servers []agentmanifest.ResolvedMCPServer
		want    []string
	}{
		{
			name:    "nil servers",
			servers: nil,
			want:    nil,
		},
		{
			name:    "empty servers",
			servers: []agentmanifest.ResolvedMCPServer{},
			want:    nil,
		},
		{
			name: "only registry build servers",
			servers: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "srv1", Build: "registry/srv1"},
				{Type: "command", Name: "srv2", Build: "registry/srv2"},
			},
			want: []string{"srv1", "srv2"},
		},
		{
			name: "only OCI servers",
			servers: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "oci1", Image: "ghcr.io/org/oci1:latest"},
				{Type: "command", Name: "oci2", Image: "ghcr.io/org/oci2:latest"},
			},
			want: nil,
		},
		{
			name: "mixed registry build and OCI servers",
			servers: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "build-me", Build: "registry/build-me"},
				{Type: "command", Name: "oci", Image: "ghcr.io/org/oci:latest"},
				{Type: "command", Name: "also-build", Build: "registry/also-build"},
			},
			want: []string{"build-me", "also-build"},
		},
		{
			name: "non-command types are excluded",
			servers: []agentmanifest.ResolvedMCPServer{
				{Type: "remote", Name: "remote-srv"},
				{Type: "registry", Name: "reg-srv"},
				{Type: "command", Name: "cmd-srv", Build: "registry/cmd-srv"},
			},
			want: []string{"cmd-srv"},
		},
		{
			name: "command with non-registry build path excluded",
			servers: []agentmanifest.ResolvedMCPServer{
				{Type: "command", Name: "local", Build: "./local-dir"},
				{Type: "command", Name: "reg", Build: "registry/reg"},
			},
			want: []string{"reg"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filterServersToBuild(tt.servers)
			if len(got) != len(tt.want) {
				t.Fatalf("filterServersToBuild() returned %d servers, want %d", len(got), len(tt.want))
			}
			for i, srv := range got {
				if srv.Name != tt.want[i] {
					t.Errorf("filterServersToBuild()[%d].Name = %q, want %q", i, srv.Name, tt.want[i])
				}
			}
		})
	}
}

// TestAgentInitRendersDockerComposeWithPort verifies that the scaffold flow
// (GenerateProject via PythonGenerator) correctly renders docker-compose.yaml
// with the Port field from AgentConfig. This ensures the {{.Port}} template
// variable works for the static file written to disk during `arctl init agent`.
func TestAgentInitRendersDockerComposeWithPort(t *testing.T) {
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		t.Fatalf("failed to read docker-compose template: %v", err)
	}

	config := common.AgentConfig{
		Name:          "test-agent",
		Image:         "docker.io/org/test-agent:latest",
		ModelProvider: "openai",
		ModelName:     "gpt-4o",
		Port:          8080,
	}

	rendered, err := gen.RenderTemplate(string(templateBytes), config)
	if err != nil {
		t.Fatalf("failed to render docker-compose template with AgentConfig: %v", err)
	}

	if !strings.Contains(rendered, "\"8080:8080\"") {
		t.Fatalf("expected init agent docker-compose to contain \"8080:8080\", got:\n%s", rendered)
	}

	// Verify that a zero Port value would produce "0:8080" (ensuring the field is used)
	config.Port = 0
	rendered, err = gen.RenderTemplate(string(templateBytes), config)
	if err != nil {
		t.Fatalf("failed to render docker-compose template with Port=0: %v", err)
	}
	if !strings.Contains(rendered, "\"0:8080\"") {
		t.Fatalf("expected Port=0 to render as \"0:8080\", got:\n%s", rendered)
	}
}

package python

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
)

func TestAgentPyTemplate_AllProviders(t *testing.T) {
	gen := NewPythonGenerator()

	tests := []struct {
		name          string
		provider      string
		modelName     string
		wantStrings   []string
		unwantStrings []string
	}{
		{
			name:          "agentgateway",
			provider:      "agentgateway",
			modelName:     "gpt-4",
			wantStrings:   []string{"BaseOpenAI", "GATEWAY_API_BASE_URL", "gpt-4"},
			unwantStrings: []string{"LiteLlm"},
		},
		{
			name:          "gemini",
			provider:      "gemini",
			modelName:     "gemini-2.0-flash",
			wantStrings:   []string{`"gemini-2.0-flash"`},
			unwantStrings: []string{"LiteLlm", "BaseOpenAI"},
		},
		{
			name:          "openai",
			provider:      "openai",
			modelName:     "gpt-4o-mini",
			wantStrings:   []string{"LiteLlm", "openai/gpt-4o-mini"},
			unwantStrings: []string{"BaseOpenAI"},
		},
		{
			name:          "anthropic",
			provider:      "anthropic",
			modelName:     "claude-3-5-sonnet",
			wantStrings:   []string{"LiteLlm", "anthropic/claude-3-5-sonnet"},
			unwantStrings: []string{"BaseOpenAI"},
		},
		{
			name:          "azureopenai",
			provider:      "azureopenai",
			modelName:     "my-deployment",
			wantStrings:   []string{"LiteLlm", "azure/my-deployment"},
			unwantStrings: []string{"BaseOpenAI"},
		},
		{
			name:          "fallback",
			provider:      "",
			modelName:     "custom-model",
			wantStrings:   []string{"custom-model"},
			unwantStrings: []string{"BaseOpenAI"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmplBytes, err := gen.ReadTemplateFile("agent/agent.py.tmpl")
			if err != nil {
				t.Fatalf("ReadTemplateFile: %v", err)
			}

			cfg := common.AgentConfig{
				Name:          "test-agent",
				ModelProvider: tt.provider,
				ModelName:     tt.modelName,
				Description:   "A test agent",
				Instruction:   "You are a helpful assistant.",
			}

			rendered, err := gen.RenderTemplate(string(tmplBytes), cfg)
			if err != nil {
				t.Fatalf("RenderTemplate: %v", err)
			}

			for _, want := range tt.wantStrings {
				if !strings.Contains(rendered, want) {
					t.Errorf("rendered output missing expected string %q", want)
				}
			}

			for _, unwant := range tt.unwantStrings {
				if strings.Contains(rendered, unwant) {
					t.Errorf("rendered output contains unwanted string %q", unwant)
				}
			}

			assertValidPython(t, rendered)
		})
	}
}

func TestOtherPyTemplates(t *testing.T) {
	gen := NewPythonGenerator()

	templates := []string{
		"agent/__init__.py.tmpl",
		"agent/mcp_tools.py.tmpl",
		"agent/prompts_loader.py.tmpl",
	}

	cfg := common.AgentConfig{
		Name:          "test-agent",
		ModelProvider: "gemini",
		ModelName:     "gemini-2.0-flash",
		Description:   "A test agent",
		Instruction:   "You are a helpful assistant.",
	}

	for _, tmplPath := range templates {
		t.Run(tmplPath, func(t *testing.T) {
			tmplBytes, err := gen.ReadTemplateFile(tmplPath)
			if err != nil {
				t.Fatalf("ReadTemplateFile(%s): %v", tmplPath, err)
			}

			rendered, err := gen.RenderTemplate(string(tmplBytes), cfg)
			if err != nil {
				t.Fatalf("RenderTemplate(%s): %v", tmplPath, err)
			}

			if len(strings.TrimSpace(rendered)) == 0 {
				t.Errorf("rendered output for %s is empty", tmplPath)
			}

			assertValidPython(t, rendered)
		})
	}
}

func assertValidPython(t *testing.T, code string) {
	t.Helper()

	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Log("python3 not found on PATH; skipping syntax validation")
		return
	}

	dir := t.TempDir()
	filePath := filepath.Join(dir, "check.py")
	if err := os.WriteFile(filePath, []byte(code), 0o644); err != nil {
		t.Fatalf("failed to write temp file: %v", err)
	}

	cmd := exec.Command(pythonPath, "-c",
		`import ast, sys; ast.parse(open(sys.argv[1]).read())`, filePath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Errorf("Python syntax validation failed:\n%s\n--- rendered code ---\n%s", string(out), code)
	}
}

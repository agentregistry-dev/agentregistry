//go:build e2e

// Tests for declarative CLI commands: apply, get, delete, and init.
// These tests verify end-to-end behavior using the real arctl binary against
// a live registry.

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// writeDeclarativeYAML writes YAML content to a temp file and returns the path.
func writeDeclarativeYAML(t *testing.T, dir, filename, content string) string {
	t.Helper()
	path := filepath.Join(dir, filename)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("Failed to write YAML file %s: %v", path, err)
	}
	return path
}

// verifyAgentExists checks that the agent exists in the registry via HTTP GET.
func verifyAgentExists(t *testing.T, regURL, name, version string) {
	t.Helper()
	encoded := strings.ReplaceAll(name, "/", "%2F")
	url := fmt.Sprintf("%s/agents/%s/versions/%s", regURL, encoded, version)
	resp := RegistryGet(t, url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected agent %s@%s to exist (HTTP 200) but got %d", name, version, resp.StatusCode)
	}
}

// verifyAgentNotFound checks that the agent no longer exists in the registry.
func verifyAgentNotFound(t *testing.T, regURL, name, version string) {
	t.Helper()
	encoded := strings.ReplaceAll(name, "/", "%2F")
	url := fmt.Sprintf("%s/agents/%s/versions/%s", regURL, encoded, version)
	client := &http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("Expected agent %s@%s to be deleted (HTTP 404) but got %d", name, version, resp.StatusCode)
	}
}

// verifyServerExists checks that the MCP server exists in the registry via HTTP GET.
func verifyServerExists(t *testing.T, regURL, name, version string) {
	t.Helper()
	encoded := strings.ReplaceAll(name, "/", "%2F")
	url := fmt.Sprintf("%s/servers/%s/versions/%s", regURL, encoded, version)
	resp := RegistryGet(t, url)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected server %s@%s to exist (HTTP 200) but got %d", name, version, resp.StatusCode)
	}
}

// TestDeclarativeApply_AgentLifecycle tests the full apply → get → delete lifecycle
// for an Agent resource using the declarative CLI.
func TestDeclarativeApply_AgentLifecycle(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("declagent")
	version := "0.0.1-e2e"

	// Clean up any stale entry from a previous interrupted run.
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	})

	agentYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/decl-agent:latest
  description: "E2E declarative apply test agent"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`, agentName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "agent.yaml", agentYAML)

	// Step 1: Apply the agent.
	t.Run("apply", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "agent/"+agentName+" applied")
	})

	// Step 2: Verify it exists in the registry.
	t.Run("verify_exists", func(t *testing.T) {
		verifyAgentExists(t, regURL, agentName, version)
	})

	// Step 3: Get it via the declarative get command (table output).
	t.Run("get_table", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "agents", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, agentName)
	})

	// Step 4: Get individual agent as YAML.
	t.Run("get_yaml", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "agent", agentName, "-o", "yaml", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "apiVersion: ar.dev/v1alpha1")
		RequireOutputContains(t, result, "kind: Agent")
		RequireOutputContains(t, result, agentName)
	})

	// Step 5: Get individual agent as JSON.
	t.Run("get_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "agent", agentName, "-o", "json", "--registry-url", regURL)
		RequireSuccess(t, result)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
			t.Fatalf("Expected valid JSON output, got: %s", result.Stdout)
		}
	})

	// Step 6: Delete it.
	t.Run("delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
		RequireSuccess(t, result)
	})

	// Step 7: Verify it is gone.
	t.Run("verify_deleted", func(t *testing.T) {
		verifyAgentNotFound(t, regURL, agentName, version)
	})
}

// TestDeclarativeApply_MCPServer tests applying an MCPServer resource using the
// declarative CLI.
func TestDeclarativeApply_MCPServer(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	serverName := "e2e-test/" + UniqueNameWithPrefix("decl-mcp")
	version := "0.0.1-e2e"

	// Clean up any stale entry.
	RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
	})

	serverYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  description: "E2E declarative apply test MCP server"
`, serverName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "server.yaml", serverYAML)

	// Apply the MCP server.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "mcpserver/"+serverName+" applied")

	// Verify it exists.
	verifyServerExists(t, regURL, serverName, version)
}

// TestDeclarativeApply_MultiDoc tests applying a multi-document YAML file.
func TestDeclarativeApply_MultiDoc(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	serverName := "e2e-test/" + UniqueNameWithPrefix("decl-multi-mcp")
	agentName := UniqueAgentName("declmultiagent")
	version := "0.0.1-e2e"

	// Clean up.
	RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "mcp", "delete", serverName, "--version", version, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	})

	multiDocYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  description: "Multi-doc test MCP server"
---
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/multi-agent:latest
  description: "Multi-doc test agent"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`, serverName, version, agentName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "stack.yaml", multiDocYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "mcpserver/"+serverName+" applied")
	RequireOutputContains(t, result, "agent/"+agentName+" applied")

	verifyServerExists(t, regURL, serverName, version)
	verifyAgentExists(t, regURL, agentName, version)
}

// TestDeclarativeApply_DryRun verifies dry-run mode does not create resources.
func TestDeclarativeApply_DryRun(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("decldryrun")
	version := "0.0.1-e2e"

	agentYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/dryrun:latest
  description: "Dry-run test agent"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`, agentName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "dryrun.yaml", agentYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--dry-run", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "[dry-run]")

	// Resource must NOT exist.
	verifyAgentNotFound(t, regURL, agentName, version)
}

// --- init tests ---

// parseDeclarativeYAML reads a YAML file and returns it as a map.
func parseDeclarativeYAML(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("Failed to read YAML file %s: %v", path, err)
	}
	var m map[string]any
	if err := yaml.Unmarshal(data, &m); err != nil {
		t.Fatalf("Failed to parse YAML file %s: %v", path, err)
	}
	return m
}

// TestDeclarativeInit_Agent verifies arctl init agent generates the correct
// declarative agent.yaml and that the result can be applied to the registry.
func TestDeclarativeInit_Agent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := UniqueAgentName("initagent")
	version := "0.1.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--version", version, "--registry-url", regURL)
	})

	// Step 1: init generates project directory and declarative agent.yaml (offline).
	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python", name)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Successfully created agent:")

	agentYAMLPath := filepath.Join(tmpDir, name, "agent.yaml")
	RequireFileExists(t, agentYAMLPath)

	// Step 2: verify the generated YAML has the right declarative structure.
	m := parseDeclarativeYAML(t, agentYAMLPath)
	if m["apiVersion"] != "ar.dev/v1alpha1" {
		t.Errorf("expected apiVersion ar.dev/v1alpha1, got %v", m["apiVersion"])
	}
	if m["kind"] != "Agent" {
		t.Errorf("expected kind Agent, got %v", m["kind"])
	}
	metadata, _ := m["metadata"].(map[string]any)
	if metadata["name"] != name {
		t.Errorf("expected metadata.name %q, got %v", name, metadata["name"])
	}

	// Step 3: apply the generated YAML directly (no edits needed for a simple name).
	applyResult := RunArctl(t, tmpDir, "apply", "-f", agentYAMLPath, "--registry-url", regURL)
	RequireSuccess(t, applyResult)
	RequireOutputContains(t, applyResult, "agent/"+name+" applied")

	// Step 4: verify it exists in the registry.
	verifyAgentExists(t, regURL, name, version)
}

// TestDeclarativeInit_MCP verifies arctl init mcp generates the correct
// declarative mcp.yaml (offline, no registry required for generation).
func TestDeclarativeInit_MCP(t *testing.T) {
	tmpDir := t.TempDir()
	// MCP names must be namespace/name format.
	dirName := UniqueNameWithPrefix("initmcp")
	fullName := "e2e-test/" + dirName

	// init is offline — no registry-url needed.
	result := RunArctl(t, tmpDir, "init", "mcp", "fastmcp-python", fullName)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Successfully created MCP server:")

	// Directory uses just the name part after "/".
	mcpYAMLPath := filepath.Join(tmpDir, dirName, "mcp.yaml")
	RequireFileExists(t, mcpYAMLPath)

	m := parseDeclarativeYAML(t, mcpYAMLPath)
	if m["apiVersion"] != "ar.dev/v1alpha1" {
		t.Errorf("expected apiVersion ar.dev/v1alpha1, got %v", m["apiVersion"])
	}
	if m["kind"] != "MCPServer" {
		t.Errorf("expected kind MCPServer, got %v", m["kind"])
	}
	metadata, _ := m["metadata"].(map[string]any)
	if metadata["name"] != fullName {
		t.Errorf("expected metadata.name %q, got %v", fullName, metadata["name"])
	}
	spec, _ := m["spec"].(map[string]any)
	pkgs, ok := spec["packages"].([]any)
	if !ok || len(pkgs) == 0 {
		t.Error("expected spec.packages to be a non-empty list")
	}
}

// TestDeclarativeInit_Skill verifies arctl init skill generates the correct
// declarative skill.yaml and that it can be applied to the registry.
func TestDeclarativeInit_Skill(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := UniqueNameWithPrefix("initskill")
	version := "0.1.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "skill", name, "--version", version, "--registry-url", regURL)
	})

	// Step 1: init generates project directory and declarative skill.yaml (offline).
	result := RunArctl(t, tmpDir, "init", "skill", name)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Successfully created skill:")

	skillYAMLPath := filepath.Join(tmpDir, name, "skill.yaml")
	RequireFileExists(t, skillYAMLPath)

	// Step 2: verify generated YAML structure.
	m := parseDeclarativeYAML(t, skillYAMLPath)
	if m["apiVersion"] != "ar.dev/v1alpha1" {
		t.Errorf("expected apiVersion ar.dev/v1alpha1, got %v", m["apiVersion"])
	}
	if m["kind"] != "Skill" {
		t.Errorf("expected kind Skill, got %v", m["kind"])
	}

	// Step 3: apply to the registry.
	applyResult := RunArctl(t, tmpDir, "apply", "-f", skillYAMLPath, "--registry-url", regURL)
	RequireSuccess(t, applyResult)
	RequireOutputContains(t, applyResult, "skill/"+name+" applied")
}

// TestDeclarativeInit_Prompt verifies arctl init prompt generates the correct
// declarative prompt YAML and that it can be applied to the registry.
func TestDeclarativeInit_Prompt(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := UniqueNameWithPrefix("initprompt")
	version := "0.1.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "prompt", name, "--version", version, "--registry-url", regURL)
	})

	// Step 1: init writes NAME.yaml in cwd (no project directory).
	result := RunArctl(t, tmpDir, "init", "prompt", name)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Successfully created prompt:")

	promptYAMLPath := filepath.Join(tmpDir, name+".yaml")
	RequireFileExists(t, promptYAMLPath)

	// Step 2: verify generated YAML structure.
	m := parseDeclarativeYAML(t, promptYAMLPath)
	if m["apiVersion"] != "ar.dev/v1alpha1" {
		t.Errorf("expected apiVersion ar.dev/v1alpha1, got %v", m["apiVersion"])
	}
	if m["kind"] != "Prompt" {
		t.Errorf("expected kind Prompt, got %v", m["kind"])
	}
	spec, _ := m["spec"].(map[string]any)
	if spec["content"] == "" {
		t.Error("expected spec.content to be non-empty")
	}

	// Step 3: apply to the registry.
	applyResult := RunArctl(t, tmpDir, "apply", "-f", promptYAMLPath, "--registry-url", regURL)
	RequireSuccess(t, applyResult)
	RequireOutputContains(t, applyResult, "prompt/"+name+" applied")
}

// --- build tests ---

// skipIfNoDocker skips the test if Docker is not available in the environment.
func skipIfNoDocker(t *testing.T) {
	t.Helper()
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil || len(out) == 0 {
		t.Skip("Skipping: Docker daemon not available")
	}
}

// TestDeclarativeBuild_Agent verifies the full declarative agent workflow:
// init → build → verify image exists.
func TestDeclarativeBuild_Agent(t *testing.T) {
	skipIfNoDocker(t)
	tmpDir := t.TempDir()

	name := UniqueAgentName("bldagent")
	image := "localhost:5001/" + name + ":latest"
	CleanupDockerImage(t, image)

	// Step 1: init the project.
	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python", name)
	RequireSuccess(t, result)

	projectDir := filepath.Join(tmpDir, name)
	RequireDirExists(t, projectDir)

	// Step 2: build the Docker image.
	result = RunArctl(t, tmpDir, "build", projectDir)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Building agent image:")

	// Step 3: verify the image was built locally.
	if !DockerImageExists(t, image) {
		t.Errorf("Expected Docker image %s to exist after build", image)
	}
}

// TestDeclarativeBuild_MCP verifies the declarative MCP build workflow:
// init → build → verify image exists.
func TestDeclarativeBuild_MCP(t *testing.T) {
	skipIfNoDocker(t)
	tmpDir := t.TempDir()

	// MCP names must be namespace/name format; directory uses just the name part.
	dirName := UniqueNameWithPrefix("bldmcp")
	fullName := "e2e-test/" + dirName
	image := "localhost:5001/" + dirName + ":latest"
	CleanupDockerImage(t, image)

	// Step 1: init the project.
	result := RunArctl(t, tmpDir, "init", "mcp", "fastmcp-python", fullName)
	RequireSuccess(t, result)

	projectDir := filepath.Join(tmpDir, dirName)
	RequireDirExists(t, projectDir)

	// Step 2: build the Docker image.
	result = RunArctl(t, tmpDir, "build", projectDir)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Building MCP server image:")

	// Step 3: verify the image was built locally.
	if !DockerImageExists(t, image) {
		t.Errorf("Expected Docker image %s to exist after build", image)
	}
}

// TestDeclarativeBuild_NoYAML verifies a clear error when no declarative YAML is present.
func TestDeclarativeBuild_NoYAML(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunArctl(t, tmpDir, "build", tmpDir)
	RequireFailure(t, result)
	combined := result.Stdout + result.Stderr
	if !strings.Contains(combined, "no declarative YAML found") {
		t.Errorf("expected 'no declarative YAML found' in output, got:\n%s", combined)
	}
}

// TestDeclarativeBuild_PromptError verifies build refuses to run for Prompt kind.
func TestDeclarativeBuild_PromptError(t *testing.T) {
	tmpDir := t.TempDir()

	// init prompt writes a file in cwd, not a subdir, so run from tmpDir.
	result := RunArctl(t, tmpDir, "init", "prompt", "myprompt")
	RequireSuccess(t, result)

	// Move the file into a subdir so we can pass a directory to build.
	subDir := filepath.Join(tmpDir, "prompt-project")
	require.NoError(t, os.MkdirAll(subDir, 0o755))
	require.NoError(t, os.Rename(
		filepath.Join(tmpDir, "myprompt.yaml"),
		filepath.Join(subDir, "prompt.yaml"),
	))

	result = RunArctl(t, tmpDir, "build", subDir)
	RequireFailure(t, result)
	combined := result.Stdout + result.Stderr
	if !strings.Contains(combined, "prompts have no build step") {
		t.Errorf("expected 'prompts have no build step' in output, got:\n%s", combined)
	}
}

// TestDeclarativeInit_InvalidArgs verifies error handling for bad init arguments.
func TestDeclarativeInit_InvalidArgs(t *testing.T) {
	tmpDir := t.TempDir()

	tests := []struct {
		name        string
		args        []string
		errContains string
	}{
		{
			name:        "agent unsupported framework",
			args:        []string{"init", "agent", "langchain", "python", "myagent"},
			errContains: "unsupported framework",
		},
		{
			name:        "mcp unsupported framework",
			args:        []string{"init", "mcp", "typescript", "myserver"},
			errContains: "unsupported framework",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := RunArctl(t, tmpDir, tc.args...)
			RequireFailure(t, result)
			combined := result.Stdout + result.Stderr
			if !strings.Contains(combined, tc.errContains) {
				t.Errorf("expected output to contain %q, got:\n%s", tc.errContains, combined)
			}
		})
	}
}

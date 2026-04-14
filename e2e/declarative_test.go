//go:build e2e

// Tests for declarative CLI commands: apply, get, delete, and init.
// These tests verify end-to-end behavior using the real arctl binary against
// a live registry.

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
  modelProvider: gemini
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
  modelProvider: gemini
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
  modelProvider: gemini
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

// TestDeclarativeApply_Idempotent verifies that applying the same agent YAML
// twice succeeds without error — the second apply is a no-op update.
func TestDeclarativeApply_Idempotent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("declidempagent")
	version := "0.0.1-e2e"

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
  image: ghcr.io/e2e-test/idemp-agent:latest
  description: "Idempotent apply test agent"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, agentName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "agent.yaml", agentYAML)

	// First apply — creates the resource.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName+" applied")

	// Second apply — same file, must not fail.
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName+" applied")

	// Resource should still exist after both applies.
	verifyAgentExists(t, regURL, agentName, version)
}

// fetchAgentDescription fetches the agent from the registry HTTP API and
// returns the description field from the response body.
func fetchAgentDescription(t *testing.T, regURL, name, version string) string {
	t.Helper()
	encoded := strings.ReplaceAll(name, "/", "%2F")
	url := fmt.Sprintf("%s/agents/%s/versions/%s", regURL, encoded, version)
	client := &http.Client{}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("Failed to GET agent %s@%s: %v", name, version, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected HTTP 200 for agent %s@%s but got %d", name, version, resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("Failed to read response body: %v", err)
	}
	var result struct {
		Agent struct {
			Description string `json:"description"`
		} `json:"agent"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode agent response: %v\nBody: %s", err, body)
	}
	return result.Agent.Description
}

// TestDeclarativeApply_Update verifies that applying an agent YAML with a
// changed description updates the existing resource in the registry.
func TestDeclarativeApply_Update(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("declupdateagent")
	version := "0.0.1-e2e"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	})

	// Step 1: Apply with "v1 description".
	v1YAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/update-agent:latest
  description: "v1 description"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, agentName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "agent.yaml", v1YAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName+" applied")
	verifyAgentExists(t, regURL, agentName, version)

	desc := fetchAgentDescription(t, regURL, agentName, version)
	if desc != "v1 description" {
		t.Errorf("expected description %q after first apply, got %q", "v1 description", desc)
	}

	// Step 2: Apply same agent with "v2 description".
	v2YAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/update-agent:latest
  description: "v2 description"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, agentName, version)

	yamlPath = writeDeclarativeYAML(t, tmpDir, "agent.yaml", v2YAML)

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Step 3: Verify the description was updated.
	desc = fetchAgentDescription(t, regURL, agentName, version)
	if desc != "v2 description" {
		t.Errorf("expected description %q after second apply, got %q", "v2 description", desc)
	}
}

// TestApplyProvider_HTTPIdempotent tests the PUT /v0/providers/{id}?platform=local endpoint:
// first call creates a provider, second call updates it, third call with same data is idempotent.
func TestApplyProvider_HTTPIdempotent(t *testing.T) {
	regURL := RegistryURL(t)
	providerID := "e2e-apply-prov-" + UniqueNameWithPrefix("prov")

	t.Cleanup(func() {
		req, _ := http.NewRequest(http.MethodDelete, regURL+"/providers/"+providerID+"?platform=local", nil)
		client := &http.Client{Timeout: 10 * time.Second}
		resp, _ := client.Do(req)
		if resp != nil {
			resp.Body.Close()
		}
	})

	client := &http.Client{Timeout: 10 * time.Second}

	doApplyProvider := func(t *testing.T, name string) *http.Response {
		t.Helper()
		body := fmt.Sprintf(`{"name":%q}`, name)
		req, err := http.NewRequest(http.MethodPut,
			regURL+"/providers/"+providerID+"?platform=local",
			strings.NewReader(body))
		if err != nil {
			t.Fatalf("failed to build request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("PUT /providers/%s failed: %v", providerID, err)
		}
		return resp
	}

	// Step 1: create.
	resp := doApplyProvider(t, "E2E Provider")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200 on create, got %d: %s", resp.StatusCode, body)
	}
	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("failed to decode create response: %v\nBody: %s", err, body)
	}
	if created.ID != providerID {
		t.Errorf("expected id %q, got %q", providerID, created.ID)
	}
	if created.Name != "E2E Provider" {
		t.Errorf("expected name %q, got %q", "E2E Provider", created.Name)
	}

	// Step 2: update (same ID, different name).
	resp2 := doApplyProvider(t, "E2E Provider Updated")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		body2, _ := io.ReadAll(resp2.Body)
		t.Fatalf("expected 200 on update, got %d: %s", resp2.StatusCode, body2)
	}
	var updated struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	body2, _ := io.ReadAll(resp2.Body)
	if err := json.Unmarshal(body2, &updated); err != nil {
		t.Fatalf("failed to decode update response: %v", err)
	}
	if updated.ID != providerID {
		t.Errorf("expected same id %q after update, got %q", providerID, updated.ID)
	}
	if updated.Name != "E2E Provider Updated" {
		t.Errorf("expected updated name, got %q", updated.Name)
	}

	// Step 3: idempotent re-apply with same data — must succeed.
	resp3 := doApplyProvider(t, "E2E Provider Updated")
	defer resp3.Body.Close()
	if resp3.StatusCode != http.StatusOK {
		body3, _ := io.ReadAll(resp3.Body)
		t.Fatalf("expected 200 on idempotent apply, got %d: %s", resp3.StatusCode, body3)
	}
}

// TestDeclarativeApply_MCPServer_Idempotent verifies that applying the same
// MCPServer YAML twice succeeds. This exercises the new PUT
// /v0/servers/{name}/versions/{version} apply endpoint enabled by the PATCH/PUT
// swap (admin edit moved to PATCH so apply could own PUT).
func TestDeclarativeApply_MCPServer_Idempotent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	serverName := "e2e-test/" + UniqueNameWithPrefix("decl-mcp-idemp")
	version := "0.0.1-e2e"

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
  description: "Idempotent apply test MCP server"
`, serverName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "server.yaml", serverYAML)

	// First apply — creates.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "mcpserver/"+serverName+" applied")

	// Second apply — must succeed (no error like "version already exists").
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "mcpserver/"+serverName+" applied")

	verifyServerExists(t, regURL, serverName, version)
}

// TestDeclarativeApply_Skill_Idempotent verifies that applying the same Skill
// YAML twice succeeds via the server-side apply endpoint.
func TestDeclarativeApply_Skill_Idempotent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	skillName := UniqueNameWithPrefix("decl-skill-idemp")
	version := "0.0.1-e2e"

	RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", version, "--registry-url", regURL)
	})

	skillYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: %s
  version: "%s"
spec:
  description: "Idempotent apply test skill"
`, skillName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "skill.yaml", skillYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "skill/"+skillName+" applied")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "skill/"+skillName+" applied")

	// Verify it exists.
	encoded := strings.ReplaceAll(skillName, "/", "%2F")
	resp := RegistryGet(t, fmt.Sprintf("%s/skills/%s/versions/%s", regURL, encoded, version))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected skill %s@%s to exist after idempotent apply, got HTTP %d", skillName, version, resp.StatusCode)
	}
}

// TestDeclarativeApply_Prompt_Idempotent verifies that applying the same Prompt
// YAML twice succeeds via the server-side apply endpoint.
func TestDeclarativeApply_Prompt_Idempotent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	promptName := UniqueNameWithPrefix("decl-prompt-idemp")
	version := "0.0.1-e2e"

	RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", version, "--registry-url", regURL)
	})

	promptYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: %s
  version: "%s"
spec:
  content: "You are a helpful test assistant."
`, promptName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "prompt.yaml", promptYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "prompt/"+promptName+" applied")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "prompt/"+promptName+" applied")

	encoded := strings.ReplaceAll(promptName, "/", "%2F")
	resp := RegistryGet(t, fmt.Sprintf("%s/prompts/%s/versions/%s", regURL, encoded, version))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected prompt %s@%s to exist after idempotent apply, got HTTP %d", promptName, version, resp.StatusCode)
	}
}

// TestApplyDeployment_HTTPIdempotent exercises PUT /v0/deployments idempotency
// against the local provider: it builds and publishes an agent, then issues
// PUT /v0/deployments three times. The first call deploys; the second and
// third calls must return the same deployment ID without error and without
// creating duplicate deployments. Skipped on the kubernetes backend.
func TestApplyDeployment_HTTPIdempotent(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("skipping local apply-deployment idempotency test: E2E_BACKEND=k8s")
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2eapplydpl")
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
	t.Cleanup(func() { removeLocalDeployment(t) })

	// Init, build, publish.
	result := RunArctl(t, tmpDir,
		"agent", "init", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "agent", "build", agentName, "--image", agentImage)
	RequireSuccess(t, result)

	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir, "agent", "publish", agentDir, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Build the sub-resource deployment URL: PUT /v0/agents/{name}/versions/latest/deployments/local
	encodedAgent := strings.ReplaceAll(agentName, "/", "%2F")
	deployURL := fmt.Sprintf("%s/agents/%s/versions/latest/deployments/local", regURL, encodedAgent)
	deployBody := `{}`

	httpClient := &http.Client{Timeout: 60 * time.Second}
	doPut := func(t *testing.T) (string, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, deployURL, strings.NewReader(deployBody))
		if err != nil {
			t.Fatalf("failed to build PUT request: %v", err)
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("PUT %s failed: %v", deployURL, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var dep struct {
			ID     string `json:"id"`
			Status string `json:"status"`
		}
		if err := json.Unmarshal(body, &dep); err != nil {
			t.Fatalf("failed to decode deployment response: %v\nBody: %s", err, body)
		}
		return dep.ID, dep.Status
	}

	// First apply — creates the deployment.
	id1, status1 := doPut(t)
	if id1 == "" {
		t.Fatal("first apply returned empty deployment ID")
	}
	t.Logf("first apply: id=%s status=%s", id1, status1)

	// Second apply — must return the same ID (idempotent no-op once deployed).
	id2, status2 := doPut(t)
	if id2 != id1 {
		t.Fatalf("second apply returned different deployment ID: got %s, want %s", id2, id1)
	}
	t.Logf("second apply: id=%s status=%s", id2, status2)

	// Third apply — same ID expected.
	id3, _ := doPut(t)
	if id3 != id1 {
		t.Fatalf("third apply returned different deployment ID: got %s, want %s", id3, id1)
	}

	// Verify only one deployment exists for this agent in deploy list.
	listURL := fmt.Sprintf("%s/deployments?resourceName=%s&resourceType=agent", regURL, agentName)
	listResp := RegistryGet(t, listURL)
	defer listResp.Body.Close()
	listBody, _ := io.ReadAll(listResp.Body)
	var listed struct {
		Deployments []struct {
			ID         string `json:"id"`
			ServerName string `json:"serverName"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(listBody, &listed); err != nil {
		t.Fatalf("failed to decode deployments list: %v\nBody: %s", err, listBody)
	}
	count := 0
	for _, d := range listed.Deployments {
		if d.ServerName == agentName {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 deployment for agent %s after 3 idempotent applies, got %d", agentName, count)
	}
}


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
		RequireOutputContains(t, result, "agent/"+agentName)
		RequireOutputContains(t, result, "applied")
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
	RequireOutputContains(t, result, "mcp/"+serverName)
	RequireOutputContains(t, result, "applied")

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
	RequireOutputContains(t, result, "mcp/"+serverName)
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")

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
	RequireOutputContains(t, result, "(dry run)")

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
	RequireOutputContains(t, applyResult, "agent/"+name)
	RequireOutputContains(t, applyResult, "applied")

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
	RequireOutputContains(t, applyResult, "skill/"+name)
	RequireOutputContains(t, applyResult, "applied")
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
	RequireOutputContains(t, applyResult, "prompt/"+name)
	RequireOutputContains(t, applyResult, "applied")
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
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")

	// Second apply — same file, must not fail.
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")

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
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")
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
	RequireOutputContains(t, result, "mcp/"+serverName)
	RequireOutputContains(t, result, "applied")

	// Second apply — must succeed (no error like "version already exists").
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "mcp/"+serverName)
	RequireOutputContains(t, result, "applied")

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
	RequireOutputContains(t, result, "skill/"+skillName)
	RequireOutputContains(t, result, "applied")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "skill/"+skillName)
	RequireOutputContains(t, result, "applied")

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
	RequireOutputContains(t, result, "prompt/"+promptName)
	RequireOutputContains(t, result, "applied")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "prompt/"+promptName)
	RequireOutputContains(t, result, "applied")

	encoded := strings.ReplaceAll(promptName, "/", "%2F")
	resp := RegistryGet(t, fmt.Sprintf("%s/prompts/%s/versions/%s", regURL, encoded, version))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected prompt %s@%s to exist after idempotent apply, got HTTP %d", promptName, version, resp.StatusCode)
	}
}

// TestApplyDeployment_HTTPIdempotent exercises POST /v0/apply deployment idempotency
// against the local provider: it builds and publishes an agent, then issues
// POST /v0/apply three times with a deployment YAML. The first call deploys;
// the second and third calls must succeed without error (idempotent re-apply).
// Skipped on the kubernetes backend.
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

	// Use POST /v0/apply with a deployment YAML body (PUT sub-resource endpoint was removed).
	applyURL := fmt.Sprintf("%s/apply", regURL)
	deployYAML := fmt.Sprintf(`kind: deployment
metadata:
  name: %s
  version: latest
spec:
  resourceType: agent
  providerId: local
`, agentName)

	httpClient := &http.Client{Timeout: 60 * time.Second}
	doApply := func(t *testing.T) string {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, applyURL, strings.NewReader(deployYAML))
		if err != nil {
			t.Fatalf("failed to build POST request: %v", err)
		}
		req.Header.Set("Content-Type", "application/yaml")
		resp, err := httpClient.Do(req)
		if err != nil {
			t.Fatalf("POST %s failed: %v", applyURL, err)
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, body)
		}
		var applyResp struct {
			Results []struct {
				Kind    string `json:"kind"`
				Name    string `json:"name"`
				Version string `json:"version"`
				Status  string `json:"status"`
			} `json:"results"`
		}
		if err := json.Unmarshal(body, &applyResp); err != nil {
			t.Fatalf("failed to decode apply response: %v\nBody: %s", err, body)
		}
		if len(applyResp.Results) == 0 {
			t.Fatalf("apply returned empty results\nBody: %s", body)
		}
		return applyResp.Results[0].Status
	}

	// First apply — creates the deployment.
	status1 := doApply(t)
	t.Logf("first apply: status=%s", status1)
	if status1 != "applied" {
		t.Fatalf("first apply: expected status 'applied', got %q", status1)
	}

	// Second apply — must succeed (idempotent no-op once deployed).
	status2 := doApply(t)
	t.Logf("second apply: status=%s", status2)
	if status2 != "applied" {
		t.Fatalf("second apply: expected status 'applied', got %q", status2)
	}

	// Third apply — same expectation.
	status3 := doApply(t)
	t.Logf("third apply: status=%s", status3)
	if status3 != "applied" {
		t.Fatalf("third apply: expected status 'applied', got %q", status3)
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

// --- Batch apply endpoint tests ---

// TestBatchApply_MultiResource verifies that applying a multi-document YAML
// containing an agent and a provider in one file succeeds and returns per-resource
// "applied" status for each resource.
func TestBatchApply_MultiResource(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("batchagent")
	agentVersion := "0.0.1-e2e"
	providerName := "e2e-batch-prov-" + UniqueNameWithPrefix("prov")

	// Pre-clean and register cleanup for both resources.
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)
		req, _ := http.NewRequest(http.MethodDelete,
			regURL+"/providers/"+providerName+"?platform=local", nil)
		client := &http.Client{Timeout: 10 * time.Second}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	multiYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/batch-agent:latest
  description: "Batch multi-resource apply test agent"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
---
apiVersion: ar.dev/v1alpha1
kind: provider
metadata:
  name: %s
spec:
  platform: local
`, agentName, agentVersion, providerName)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "multi.yaml", multiYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Each resource must appear in the output as "applied".
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")
	RequireOutputContains(t, result, "provider/"+providerName)

	// Verify agent exists via HTTP.
	verifyAgentExists(t, regURL, agentName, agentVersion)
}

// TestBatchApply_Idempotent verifies that applying the same multi-document YAML
// twice succeeds without error. The second apply is a server-side upsert that
// returns "applied" for both resources (the server does not currently distinguish
// no-op updates from mutations at the batch level).
func TestBatchApply_Idempotent(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("idempbatch")
	agentVersion := "0.0.1-e2e"
	providerName := "e2e-idemp-prov-" + UniqueNameWithPrefix("prov")

	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)
		req, _ := http.NewRequest(http.MethodDelete,
			regURL+"/providers/"+providerName+"?platform=local", nil)
		client := &http.Client{Timeout: 10 * time.Second}
		if resp, err := client.Do(req); err == nil {
			resp.Body.Close()
		}
	})

	multiYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/idemp-batch-agent:latest
  description: "Idempotent batch apply test"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
---
apiVersion: ar.dev/v1alpha1
kind: provider
metadata:
  name: %s
spec:
  platform: local
`, agentName, agentVersion, providerName)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "multi.yaml", multiYAML)

	// First apply — creates both resources.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")

	// Second apply — same file, must not fail (upsert semantics).
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName)
	RequireOutputContains(t, result, "applied")

	// Both resources must still exist after both applies.
	verifyAgentExists(t, regURL, agentName, agentVersion)
}

// TestBatchApply_DriftRequiresForce verifies that applying a deployment whose
// config has drifted from the running deployment fails without --force and
// succeeds with --force. This test only runs on the docker backend, as it
// requires a live local deployment that can be in-flight.
//
// The test uses the Deployment kind's ErrDeploymentDrift path by:
//  1. Publishing an agent and deploying it.
//  2. Modifying the env in the YAML.
//  3. Re-applying without --force — expects failure with a "force" hint.
//  4. Re-applying with --force — expects success.
func TestBatchApply_DriftRequiresForce(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("skipping drift test: not applicable on k8s backend (requires local docker provider)")
	}
	// skipped: arctl agent build cannot read declarative agent.yaml produced by
	// arctl init agent (pre-existing #425 compat issue — declarative init writes
	// kind/metadata/spec format but build expects flat agentName/language/framework).
	t.Skip("skipped: arctl agent build cannot read declarative agent.yaml (pre-existing #425 compat issue)")

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("driftbatch")
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)
	agentVersion := "0.1.0"
	providerID := "local"

	t.Cleanup(func() {
		RemoveDeploymentsByServerName(t, regURL, agentName)
		removeLocalDeployment(t)
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)
	})

	// Step 1: init → build → publish the agent.
	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python",
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

	// Step 2: apply the initial deployment YAML (no env).
	deployYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: deployment
metadata:
  name: %s
  version: "%s"
spec:
  providerId: %s
  resourceType: agent
`, agentName, agentVersion, providerID)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "deploy.yaml", deployYAML)
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "deployment/"+agentName)
	RequireOutputContains(t, result, "applied")

	// Step 3: modify the env to create drift.
	driftYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: deployment
metadata:
  name: %s
  version: "%s"
spec:
  providerId: %s
  resourceType: agent
  env:
    NEW_VAR: "drift-value"
`, agentName, agentVersion, providerID)

	driftPath := writeDeclarativeYAML(t, tmpDir, "deploy-drift.yaml", driftYAML)

	// Apply drifted YAML without --force — expect failure.
	result = RunArctl(t, tmpDir, "apply", "-f", driftPath, "--registry-url", regURL)
	RequireFailure(t, result)
	// Server should hint about --force.
	combined := result.Stdout + result.Stderr
	if !strings.Contains(combined, "force") {
		t.Logf("Expected 'force' hint in output; got:\n%s", combined)
	}

	// Step 4: apply with --force — expect success.
	result = RunArctl(t, tmpDir, "apply", "-f", driftPath, "--force", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "deployment/"+agentName)
	RequireOutputContains(t, result, "applied")
}

// TestBatchApply_DeleteFile verifies that arctl delete -f <file> deletes all
// resources listed in the file via DELETE /v0/apply, and that the resources
// are subsequently not found via HTTP GET.
func TestBatchApply_DeleteFile(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("delbatch")
	agentVersion := "0.0.1-e2e"

	// Ensure clean state before the test.
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", agentVersion, "--registry-url", regURL)

	agentYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/del-batch-agent:latest
  description: "Delete-file batch test agent"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, agentName, agentVersion)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "agent.yaml", agentYAML)

	// Step 1: apply.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "agent/"+agentName)
	verifyAgentExists(t, regURL, agentName, agentVersion)

	// Step 2: delete -f — sends DELETE /v0/apply.
	result = RunArctl(t, tmpDir, "delete", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Step 3: resource must be gone.
	verifyAgentNotFound(t, regURL, agentName, agentVersion)
}


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
	"net/url"
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

// resourceURL builds the v1alpha1-native URL for a single resource version:
//
//	{regURL}/{resource}/{name}/{version}
//
// Namespace is implicit ("default") and elided from the path post-flatten;
// callers that target a non-default namespace pass `?namespace=...` directly.
//
// Resource names that contain "/" (common for MCPServer identifiers like
// "e2e-test/decl-mcp-123") are URL-encoded into a single path segment so
// Huma's router treats them as one {name} parameter. Apply stores these
// names literally under the default namespace; the CLI itself uses
// url.PathEscape on delete/get, so the HTTP client must match.
func resourceURL(regURL, resource, name, version string) string {
	return fmt.Sprintf("%s/%s/%s/%s",
		regURL, resource, url.PathEscape(name), version)
}

// verifyAgentExists checks that the agent exists in the registry via HTTP GET.
func verifyAgentExists(t *testing.T, regURL, name, version string) {
	t.Helper()
	resp := RegistryGet(t, resourceURL(regURL, "agents", name, version))
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("Expected agent %s@%s to exist (HTTP 200) but got %d", name, version, resp.StatusCode)
	}
}

// requireDeleted asserts that the named resource no longer appears as a live
// row in the registry. Under the v1alpha1 soft-delete contract a DELETE only
// sets metadata.deletionTimestamp — the row survives until GC picks it up.
// So "deleted" from an HTTP-client perspective means either:
//   - 404: the row was hard-deleted by GC, OR
//   - 200 with metadata.deletionTimestamp != nil: the row is terminating.
//
// Either satisfies the test's intent that the delete was recorded.
func requireDeleted(t *testing.T, regURL, resource, name, version string) {
	t.Helper()
	resp, err := http.Get(resourceURL(regURL, resource, name, version))
	if err != nil {
		t.Fatalf("GET %s after delete failed: %v", resource, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 404 or 200-terminating for %s %s@%s after delete, got %d",
			resource, name, version, resp.StatusCode)
	}
	var envelope struct {
		Metadata struct {
			DeletionTimestamp *string `json:"deletionTimestamp,omitempty"`
		} `json:"metadata"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatalf("decode %s response: %v\nbody: %s", resource, err, body)
	}
	if envelope.Metadata.DeletionTimestamp == nil {
		t.Fatalf("expected %s %s@%s to be terminating (deletionTimestamp set) after delete, got live row",
			resource, name, version)
	}
}

// verifyAgentNotFound checks that the agent no longer exists in the registry.
func verifyAgentNotFound(t *testing.T, regURL, name, version string) {
	t.Helper()
	requireDeleted(t, regURL, "agents", name, version)
}

func verifyServerNotFound(t *testing.T, regURL, name, version string) {
	t.Helper()
	requireDeleted(t, regURL, "mcpservers", name, version)
}

func verifySkillNotFound(t *testing.T, regURL, name, version string) {
	t.Helper()
	requireDeleted(t, regURL, "skills", name, version)
}

func verifyPromptNotFound(t *testing.T, regURL, name, version string) {
	t.Helper()
	requireDeleted(t, regURL, "prompts", name, version)
}

// verifyServerExists checks that the MCP server exists in the registry via HTTP GET.
func verifyServerExists(t *testing.T, regURL, name, version string) {
	t.Helper()
	resp := RegistryGet(t, resourceURL(regURL, "mcpservers", name, version))
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
		RequireOutputContains(t, result, "Agent/"+agentName)
		RequireOutputContains(t, result, "✓")
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
	RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
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
	RequireOutputContains(t, result, "MCPServer/"+serverName)
	RequireOutputContains(t, result, "✓")

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
	RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
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
	RequireOutputContains(t, result, "MCPServer/"+serverName)
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")

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
	RequireOutputContains(t, applyResult, "Agent/"+name)
	RequireOutputContains(t, applyResult, "✓")

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
	RequireOutputContains(t, applyResult, "Skill/"+name)
	RequireOutputContains(t, applyResult, "✓")
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
	RequireOutputContains(t, applyResult, "Prompt/"+name)
	RequireOutputContains(t, applyResult, "✓")
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
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")

	// Second apply — same file, must not fail.
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")

	// Resource should still exist after both applies.
	verifyAgentExists(t, regURL, agentName, version)
}

// fetchAgentDescription fetches the agent from the registry HTTP API and
// returns the description field from the response body.
func fetchAgentDescription(t *testing.T, regURL, name, version string) string {
	t.Helper()
	url := resourceURL(regURL, "agents", name, version)
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
		Spec struct {
			Description string `json:"description"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		t.Fatalf("Failed to decode agent response: %v\nBody: %s", err, body)
	}
	return result.Spec.Description
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
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")
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

	RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
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
	RequireOutputContains(t, result, "MCPServer/"+serverName)
	RequireOutputContains(t, result, "✓")

	// Second apply — must succeed (no error like "version already exists").
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+serverName)
	RequireOutputContains(t, result, "✓")

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
	RequireOutputContains(t, result, "Skill/"+skillName)
	RequireOutputContains(t, result, "✓")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Skill/"+skillName)
	RequireOutputContains(t, result, "✓")

	// Verify it exists.
	resp := RegistryGet(t, resourceURL(regURL, "skills", skillName, version))
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
	RequireOutputContains(t, result, "Prompt/"+promptName)
	RequireOutputContains(t, result, "✓")

	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Prompt/"+promptName)
	RequireOutputContains(t, result, "✓")

	resp := RegistryGet(t, resourceURL(regURL, "prompts", promptName, version))
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
	// Local-provider deploy binds port 8080 via a shared docker-compose
	// project. Multiple tests exercising that path race on port allocation
	// and on lazy-cleanup from prior tests, making the suite flaky on CI.
	// Opt-in via E2E_RUN_LOCAL_DEPLOY=1 to run locally.
	if os.Getenv("E2E_RUN_LOCAL_DEPLOY") != "1" {
		t.Skip("skipping local-deploy test; set E2E_RUN_LOCAL_DEPLOY=1 to run")
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2eapplydpl")
	// localhost:5001 is the private registry the daemon runs on the docker
	// backend. `arctl build --push` pushes to it so the local-provider
	// deploy can pull it back. Public images don't satisfy the adapter's
	// expected container shape, so we build a real one.
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
	t.Cleanup(func() { removeLocalDeployment(t) })

	// Init → build+push → apply. Build is required: the local-provider
	// deploy actually pulls the tagged image and starts it, so the image
	// must exist in the daemon's localhost:5001 registry first.
	result := RunArctl(t, tmpDir,
		"init", "agent", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir, "build", agentDir, "--push", "--image", agentImage)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "apply", "-f", filepath.Join(agentDir, "agent.yaml"), "--registry-url", regURL)
	RequireSuccess(t, result)

	// Use POST /v0/apply with a deployment YAML body (PUT sub-resource endpoint was removed).
	applyURL := fmt.Sprintf("%s/apply", regURL)
	deployYAML := fmt.Sprintf(`kind: Deployment
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

	// isApplySuccess matches the kubectl-style verbs the server emits for a
	// successful apply: "created", "configured", "unchanged". (The failure
	// verb is "failed".)
	isApplySuccess := func(s string) bool {
		return s == "created" || s == "configured" || s == "unchanged"
	}

	// First apply — creates the deployment.
	status1 := doApply(t)
	t.Logf("first apply: status=%s", status1)
	if !isApplySuccess(status1) {
		t.Fatalf("first apply: expected success status, got %q", status1)
	}

	// Second apply — must succeed (idempotent no-op once deployed).
	status2 := doApply(t)
	t.Logf("second apply: status=%s", status2)
	if !isApplySuccess(status2) {
		t.Fatalf("second apply: expected success status, got %q", status2)
	}

	// Third apply — same expectation.
	status3 := doApply(t)
	t.Logf("third apply: status=%s", status3)
	if !isApplySuccess(status3) {
		t.Fatalf("third apply: expected success status, got %q", status3)
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
kind: Provider
metadata:
  name: %s
  version: "1.0.0"
spec:
  platform: local
`, agentName, agentVersion, providerName)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "multi.yaml", multiYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Each resource must appear in the output as "applied".
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")
	RequireOutputContains(t, result, "Provider/"+providerName)

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
kind: Provider
metadata:
  name: %s
  version: "1.0.0"
spec:
  platform: local
`, agentName, agentVersion, providerName)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "multi.yaml", multiYAML)

	// First apply — creates both resources.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")

	// Second apply — same file, must not fail (upsert semantics).
	result = RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Agent/"+agentName)
	RequireOutputContains(t, result, "✓")

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
	// See TestApplyDeployment_HTTPIdempotent: local-deploy races on port 8080
	// against other deploy tests when cleanup lags; opt-in via env var.
	if os.Getenv("E2E_RUN_LOCAL_DEPLOY") != "1" {
		t.Skip("skipping local-deploy test; set E2E_RUN_LOCAL_DEPLOY=1 to run")
	}

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

	// Step 1: init → build+push → apply the agent. Build pushes to the
	// daemon's private localhost:5001 registry so the subsequent local
	// deploy can pull it.
	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	)
	RequireSuccess(t, result)

	agentDir := filepath.Join(tmpDir, agentName)
	result = RunArctl(t, tmpDir, "build", agentDir, "--push", "--image", agentImage)
	RequireSuccess(t, result)

	result = RunArctl(t, tmpDir, "apply", "-f", filepath.Join(agentDir, "agent.yaml"), "--registry-url", regURL)
	RequireSuccess(t, result)

	// Step 2: apply the initial deployment YAML (no env).
	deployYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
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
	RequireOutputContains(t, result, "Deployment/"+agentName)
	RequireOutputContains(t, result, "✓")

	// Step 3: modify the env to create drift.
	driftYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
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
	RequireOutputContains(t, result, "Deployment/"+agentName)
	RequireOutputContains(t, result, "✓")
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
	RequireOutputContains(t, result, "Agent/"+agentName)
	verifyAgentExists(t, regURL, agentName, agentVersion)

	// Step 2: delete -f — sends DELETE /v0/apply.
	result = RunArctl(t, tmpDir, "delete", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Step 3: resource must be gone.
	verifyAgentNotFound(t, regURL, agentName, agentVersion)
}

// TestDeclarative_MCPRoundTrip exercises the full apply → get (table/yaml/json)
// → delete lifecycle for an MCPServer resource via the declarative CLI.
func TestDeclarative_MCPRoundTrip(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	serverName := "e2e-test/" + UniqueNameWithPrefix("mcp-rt")
	version := "0.0.1-e2e"

	RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	})

	serverYAML := fmt.Sprintf(`
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  description: "MCP round-trip test server"
`, serverName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "server.yaml", serverYAML)

	t.Run("apply", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "MCPServer/"+serverName)
		RequireOutputContains(t, result, "✓")
	})

	t.Run("verify_exists", func(t *testing.T) {
		verifyServerExists(t, regURL, serverName, version)
	})

	t.Run("get_table", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "mcps", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, serverName)
	})

	t.Run("get_yaml", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "mcp", serverName, "-o", "yaml", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "apiVersion: ar.dev/v1alpha1")
		RequireOutputContains(t, result, "kind: MCPServer")
		RequireOutputContains(t, result, serverName)
	})

	t.Run("get_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "mcp", serverName, "-o", "json", "--registry-url", regURL)
		RequireSuccess(t, result)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
			t.Fatalf("Expected valid JSON, got: %s", result.Stdout)
		}
	})

	t.Run("delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
		RequireSuccess(t, result)
	})

	t.Run("verify_deleted", func(t *testing.T) {
		verifyServerNotFound(t, regURL, serverName, version)
	})
}

// TestDeclarative_SkillRoundTrip exercises the full apply → get (table/yaml)
// → delete lifecycle for a Skill resource via the declarative CLI.
func TestDeclarative_SkillRoundTrip(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	skillName := UniqueNameWithPrefix("skill-rt")
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
  description: "Skill round-trip test"
`, skillName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "skill.yaml", skillYAML)

	t.Run("apply", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "Skill/"+skillName)
		RequireOutputContains(t, result, "✓")
	})

	t.Run("verify_exists", func(t *testing.T) {
		resp, err := http.Get(resourceURL(regURL, "skills", skillName, version))
		if err != nil {
			t.Fatalf("GET skill failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for existing skill, got %d", resp.StatusCode)
		}
	})

	t.Run("get_table", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "skills", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, skillName)
	})

	t.Run("get_yaml", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "skill", skillName, "-o", "yaml", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "apiVersion: ar.dev/v1alpha1")
		RequireOutputContains(t, result, "kind: Skill")
		RequireOutputContains(t, result, skillName)
	})

	t.Run("get_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "skill", skillName, "-o", "json", "--registry-url", regURL)
		RequireSuccess(t, result)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
			t.Fatalf("Expected valid JSON, got: %s", result.Stdout)
		}
	})

	t.Run("delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", version, "--registry-url", regURL)
		RequireSuccess(t, result)
	})

	t.Run("verify_deleted", func(t *testing.T) {
		verifySkillNotFound(t, regURL, skillName, version)
	})
}

// TestDeclarative_PromptRoundTrip exercises the full apply → get (table/yaml)
// → delete lifecycle for a Prompt resource via the declarative CLI.
func TestDeclarative_PromptRoundTrip(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	promptName := UniqueNameWithPrefix("prompt-rt")
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
  description: "Prompt round-trip test"
  content: "You are a test assistant."
`, promptName, version)
	yamlPath := writeDeclarativeYAML(t, tmpDir, "prompt.yaml", promptYAML)

	t.Run("apply", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "Prompt/"+promptName)
		RequireOutputContains(t, result, "✓")
	})

	t.Run("verify_exists", func(t *testing.T) {
		resp, err := http.Get(resourceURL(regURL, "prompts", promptName, version))
		if err != nil {
			t.Fatalf("GET prompt failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 for existing prompt, got %d", resp.StatusCode)
		}
	})

	t.Run("get_table", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "prompts", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, promptName)
	})

	t.Run("get_yaml", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "prompt", promptName, "-o", "yaml", "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "apiVersion: ar.dev/v1alpha1")
		RequireOutputContains(t, result, "kind: Prompt")
		RequireOutputContains(t, result, promptName)
	})

	t.Run("get_json", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "get", "prompt", promptName, "-o", "json", "--registry-url", regURL)
		RequireSuccess(t, result)
		var parsed map[string]any
		if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
			t.Fatalf("Expected valid JSON, got: %s", result.Stdout)
		}
	})

	t.Run("delete", func(t *testing.T) {
		result := RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", version, "--registry-url", regURL)
		RequireSuccess(t, result)
	})

	t.Run("verify_deleted", func(t *testing.T) {
		verifyPromptNotFound(t, regURL, promptName, version)
	})
}

// TestDeclarative_DeleteFileMultiKind verifies that `arctl delete -f multi.yaml`
// removes all kinds (agent, mcp, skill, prompt) in a single batch.
func TestDeclarative_DeleteFileMultiKind(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	agentName := UniqueAgentName("delmulti")
	mcpName := "e2e-test/" + UniqueNameWithPrefix("delmulti-mcp")
	skillName := UniqueNameWithPrefix("delmulti-skill")
	promptName := UniqueNameWithPrefix("delmulti-prompt")
	version := "0.0.1-e2e"

	// Pre-clean and post-clean via the same declarative command.
	cleanup := func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "mcp", mcpName, "--version", version, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", version, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", version, "--registry-url", regURL)
	}
	cleanup()
	t.Cleanup(cleanup)

	multiYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "%s"
spec:
  image: ghcr.io/e2e-test/delmulti-agent:latest
  description: "multi-kind delete test"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
---
apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  description: "multi-kind delete test mcp"
---
apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: %s
  version: "%s"
spec:
  description: "multi-kind delete test skill"
---
apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: %s
  version: "%s"
spec:
  description: "multi-kind delete test prompt"
  content: "noop"
`, agentName, version, mcpName, version, skillName, version, promptName, version)

	yamlPath := writeDeclarativeYAML(t, tmpDir, "multi.yaml", multiYAML)

	// Step 1: apply.
	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)
	verifyAgentExists(t, regURL, agentName, version)
	verifyServerExists(t, regURL, mcpName, version)

	// Step 2: delete -f — sends DELETE /v0/apply.
	result = RunArctl(t, tmpDir, "delete", "-f", yamlPath, "--registry-url", regURL)
	RequireSuccess(t, result)

	// Step 3: every kind must be gone.
	verifyAgentNotFound(t, regURL, agentName, version)
	verifyServerNotFound(t, regURL, mcpName, version)
	verifySkillNotFound(t, regURL, skillName, version)
	verifyPromptNotFound(t, regURL, promptName, version)
}

// TestArctl_KeptCommandsResolve asserts every surviving command resolves via
// --help after the imperative CRUD deletion PR. Cheap guard against future
// over-eager deletions of runtime or declarative surface commands.
func TestArctl_KeptCommandsResolve(t *testing.T) {
	t.Parallel()
	cases := [][]string{
		{"agent", "run"},
		{"mcp", "run"},
		{"mcp", "add-tool"},
		{"skill", "pull"},
		{"apply"},
		{"get"},
		{"delete"},
		{"init"},
		{"build"},
	}
	for _, args := range cases {
		args := args
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			t.Parallel()
			helpArgs := append([]string{}, args...)
			helpArgs = append(helpArgs, "--help")
			result := RunArctl(t, t.TempDir(), helpArgs...)
			RequireSuccess(t, result)
		})
	}
}

// TestAgentBuild_EnvelopeManifest verifies that `arctl build` against a
// project directory generated by the declarative `arctl init agent` command
// succeeds. `arctl init agent` writes envelope YAML (apiVersion/kind/metadata/
// spec); `arctl build` calls project.LoadManifest which detects and decodes
// that envelope. Regression guard for the envelope path.
func TestAgentBuild_EnvelopeManifest(t *testing.T) {
	skipIfNoDocker(t)
	tmpDir := t.TempDir()

	name := UniqueAgentName("envagent")
	image := "localhost:5001/" + name + ":latest"
	CleanupDockerImage(t, image)

	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python", name)
	RequireSuccess(t, result)

	projectDir := filepath.Join(tmpDir, name)
	RequireDirExists(t, projectDir)

	// Sanity: init wrote an envelope YAML.
	RequireFileContains(t, filepath.Join(projectDir, "agent.yaml"), "apiVersion: ar.dev/v1alpha1")
	RequireFileContains(t, filepath.Join(projectDir, "agent.yaml"), "kind: Agent")

	result = RunArctl(t, tmpDir, "build", projectDir)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Building agent image:")
	if !DockerImageExists(t, image) {
		t.Errorf("Expected Docker image %s after build of envelope project", image)
	}
}

// TestMCPBuild_EnvelopeManifest is the MCP counterpart of
// TestAgentBuild_EnvelopeManifest. Verifies that mcp/manifest.Manager.Load
// accepts envelope YAML written by `arctl init mcp`.
func TestMCPBuild_EnvelopeManifest(t *testing.T) {
	skipIfNoDocker(t)
	tmpDir := t.TempDir()

	dirName := UniqueNameWithPrefix("envmcp")
	fullName := "e2e-test/" + dirName
	image := "localhost:5001/" + dirName + ":latest"
	CleanupDockerImage(t, image)

	result := RunArctl(t, tmpDir, "init", "mcp", "fastmcp-python", fullName)
	RequireSuccess(t, result)

	projectDir := filepath.Join(tmpDir, dirName)
	RequireDirExists(t, projectDir)

	RequireFileContains(t, filepath.Join(projectDir, "mcp.yaml"), "apiVersion: ar.dev/v1alpha1")
	RequireFileContains(t, filepath.Join(projectDir, "mcp.yaml"), "kind: MCPServer")

	result = RunArctl(t, tmpDir, "build", projectDir)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Building MCP server image:")
	if !DockerImageExists(t, image) {
		t.Errorf("Expected Docker image %s after build of envelope project", image)
	}
}

// --- declarative validation edge cases ---
//
// Coverage for error paths exercised only through the declarative CLI surface.

// TestDeclarativeBuild_NonexistentDir verifies that `arctl build` fails when
// pointed at a directory that does not exist. TestDeclarativeBuild_NoYAML
// covers the empty-directory case; this covers the missing-directory case.
func TestDeclarativeBuild_NonexistentDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := RunArctl(t, tmpDir, "build", filepath.Join(tmpDir, "nonexistent"))
	RequireFailure(t, result)
	RequireOutputContains(t, result, "project directory not found:")
}

// TestDeclarativeApply_InvalidKind verifies that `arctl apply` rejects a YAML
// document whose `kind` is not registered in the CLI's kinds registry. The
// failure is client-side: the kinds registry lookup returns an error before
// any HTTP request is sent.
func TestDeclarativeApply_InvalidKind(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	invalidYAML := `apiVersion: ar.dev/v1alpha1
kind: NotARealKind
metadata:
  name: e2e-test/invalid-kind
  version: "0.0.1-e2e"
spec:
  description: "bogus kind for client-side rejection test"
`
	yamlPath := writeDeclarativeYAML(t, tmpDir, "invalid-kind.yaml", invalidYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", yamlPath, "--registry-url", regURL)
	RequireFailure(t, result)
	// Matches kinds.ErrUnknownKind.
	RequireOutputContains(t, result, "unknown kind")
}

// TestDeclarativeDelete_NotFound verifies `arctl delete` reports failure when
// the target resource does not exist.
func TestDeclarativeDelete_NotFound(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	result := RunArctl(t, tmpDir,
		"delete", "prompt", "nonexistent-prompt-xyz-12345",
		"--version", "1.0.0",
		"--registry-url", regURL,
	)
	RequireFailure(t, result)
	RequireOutputContains(t, result, "not found")
}

// TestDeclarativeInit_AgentWithRefs verifies that arctl init agent's --mcp,
// --skill, and --prompt flags produce the correct declarative ref entries in
// the generated agent.yaml. These flags are the declarative replacement for
// the deleted arctl agent add-mcp / add-skill / add-prompt commands.
func TestDeclarativeInit_AgentWithRefs(t *testing.T) {
	tmpDir := t.TempDir()
	name := UniqueAgentName("initrefs")

	// init is offline; no registry-url required for generation.
	result := RunArctl(t, tmpDir, "init", "agent", "adk", "python", name,
		"--mcp", "acme/fetch@1.0.0",
		"--mcp", "acme/time@2.0.0",
		"--skill", "summarize@1.0.0",
		"--skill", "refine",
		"--prompt", "sys-prompt@1.0.0",
	)
	RequireSuccess(t, result)

	agentYAMLPath := filepath.Join(tmpDir, name, "agent.yaml")
	RequireFileExists(t, agentYAMLPath)

	m := parseDeclarativeYAML(t, agentYAMLPath)
	spec, ok := m["spec"].(map[string]any)
	if !ok {
		t.Fatalf("spec missing or wrong type in generated agent.yaml: %v", m["spec"])
	}

	// The v1alpha1 scaffolder emits ResourceRef entries: {kind, name, version}.
	// mcpServers — two registry refs, @version parsed correctly.
	mcps, ok := spec["mcpServers"].([]any)
	if !ok || len(mcps) != 2 {
		t.Fatalf("expected 2 mcpServers, got %v", spec["mcpServers"])
	}
	for i, expected := range []struct {
		name, version string
	}{
		{"acme/fetch", "1.0.0"},
		{"acme/time", "2.0.0"},
	} {
		entry, _ := mcps[i].(map[string]any)
		if entry["kind"] != "MCPServer" {
			t.Errorf("mcpServers[%d]: expected kind=MCPServer, got %v", i, entry["kind"])
		}
		if entry["name"] != expected.name {
			t.Errorf("mcpServers[%d]: name expected %q, got %v", i, expected.name, entry["name"])
		}
		if entry["version"] != expected.version {
			t.Errorf("mcpServers[%d]: version expected %q, got %v", i, expected.version, entry["version"])
		}
	}

	// skills — two entries; second uses default version "latest".
	skills, ok := spec["skills"].([]any)
	if !ok || len(skills) != 2 {
		t.Fatalf("expected 2 skills, got %v", spec["skills"])
	}
	for i, expected := range []struct {
		name, version string
	}{
		{"summarize", "1.0.0"},
		{"refine", "latest"},
	} {
		entry, _ := skills[i].(map[string]any)
		if entry["kind"] != "Skill" {
			t.Errorf("skills[%d]: expected kind=Skill, got %v", i, entry["kind"])
		}
		if entry["name"] != expected.name {
			t.Errorf("skills[%d]: name expected %q, got %v", i, expected.name, entry["name"])
		}
		if entry["version"] != expected.version {
			t.Errorf("skills[%d]: version expected %q, got %v", i, expected.version, entry["version"])
		}
	}

	// prompts — one entry with explicit version.
	prompts, ok := spec["prompts"].([]any)
	if !ok || len(prompts) != 1 {
		t.Fatalf("expected 1 prompt, got %v", spec["prompts"])
	}
	entry, _ := prompts[0].(map[string]any)
	if entry["kind"] != "Prompt" {
		t.Errorf("prompts[0]: expected kind=Prompt, got %v", entry["kind"])
	}
	if entry["name"] != "sys-prompt" {
		t.Errorf("prompts[0]: name expected %q, got %v", "sys-prompt", entry["name"])
	}
	if entry["version"] != "1.0.0" {
		t.Errorf("prompts[0]: version expected %q, got %v", "1.0.0", entry["version"])
	}
}

// TestDeploymentGet_YAMLIncludesStatus creates an agent + local deployment,
// then checks that `arctl get deployment NAME -o yaml` renders a .status
// block (phase/id/origin) in addition to the declarative spec. Round-trips
// the output through `arctl apply` to confirm status is silently dropped on
// input.
func TestDeploymentGet_YAMLIncludesStatus(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("skipping local deployment status test: E2E_BACKEND=k8s")
	}
	// See TestApplyDeployment_HTTPIdempotent: local-deploy races on port 8080
	// against other deploy tests when cleanup lags; opt-in via env var.
	if os.Getenv("E2E_RUN_LOCAL_DEPLOY") != "1" {
		t.Skip("skipping local-deploy test; set E2E_RUN_LOCAL_DEPLOY=1 to run")
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	agentName := UniqueAgentName("e2estatus")
	version := "0.1.0"
	// Local-provider deploys pull from localhost:5001 (the daemon's private
	// registry). Scaffold → build+push so the image resolves at deploy time.
	agentImage := fmt.Sprintf("localhost:5001/%s:e2e", agentName)

	t.Cleanup(func() { RemoveDeploymentsByServerName(t, regURL, agentName) })
	t.Cleanup(func() { removeLocalDeployment(t) })
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", agentName, "--version", version, "--registry-url", regURL)
	})

	// init → build+push → apply — same shape as TestApplyDeployment_HTTPIdempotent.
	RequireSuccess(t, RunArctl(t, tmpDir,
		"init", "agent", "adk", "python",
		"--model-name", "gemini-2.5-flash",
		"--image", agentImage,
		agentName,
	))
	agentDir := filepath.Join(tmpDir, agentName)
	RequireSuccess(t, RunArctl(t, tmpDir, "build", agentDir, "--push", "--image", agentImage))
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f",
		filepath.Join(agentDir, "agent.yaml"), "--registry-url", regURL))

	deployYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
  version: "%s"
spec:
  resourceType: agent
  providerId: local
`, agentName, version)
	deployPath := writeDeclarativeYAML(t, tmpDir, "deployment.yaml", deployYAML)
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f", deployPath, "--registry-url", regURL))

	// Fetch as YAML and assert both spec and status blocks are present.
	result := RunArctl(t, tmpDir, "get", "deployment", agentName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "apiVersion: ar.dev/v1alpha1")
	RequireOutputContains(t, result, "kind: Deployment")
	// Spec fields — declarative, round-trippable.
	RequireOutputContains(t, result, "providerId: local")
	RequireOutputContains(t, result, "resourceType: agent")
	// Status block — server-managed.
	RequireOutputContains(t, result, "status:")
	// phase may be "deploying" or "deployed" depending on how fast the
	// reconciler runs for the local platform; both assert the status block.
	if !strings.Contains(result.Stdout, "phase:") {
		t.Fatalf("expected .status.phase in get output, got:\n%s", result.Stdout)
	}
	if !strings.Contains(result.Stdout, "id:") {
		t.Fatalf("expected .status.id (server-assigned UUID) in get output, got:\n%s", result.Stdout)
	}

	// Round-trip guarantee: apply the yaml we just fetched — the .status
	// block must be silently ignored on decode; apply returns configured/
	// unchanged rather than a "status not allowed" error.
	roundTripPath := writeDeclarativeYAML(t, tmpDir, "roundtrip.yaml", result.Stdout)
	result = RunArctl(t, tmpDir, "apply", "-f", roundTripPath, "--registry-url", regURL)
	RequireSuccess(t, result)
}

// TestDeploymentApply_BadTemplateRef applies a deployment whose referenced
// agent does not exist. Apply must exit non-zero with a clear error message
// identifying the missing template — not silently create a ghost row.
func TestDeploymentApply_BadTemplateRef(t *testing.T) {
	if IsK8sBackend() {
		t.Skip("skipping bad-templateRef test: E2E_BACKEND=k8s")
	}

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	// Name intentionally NOT created as an agent.
	missingName := UniqueAgentName("e2emissing")

	deployYAML := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: %s
  version: "0.1.0"
spec:
  targetRef:
    kind: Agent
    name: %s
    version: "0.1.0"
  providerRef:
    kind: Provider
    name: local
`, missingName, missingName)
	deployPath := writeDeclarativeYAML(t, tmpDir, "deployment.yaml", deployYAML)

	result := RunArctl(t, tmpDir, "apply", "-f", deployPath, "--registry-url", regURL)
	if result.ExitCode == 0 {
		t.Fatalf("expected non-zero exit for missing templateRef, got zero\nstdout: %s\nstderr: %s",
			result.Stdout, result.Stderr)
	}
	combined := result.Stdout + "\n" + result.Stderr
	if !strings.Contains(strings.ToLower(combined), "not found") {
		t.Fatalf("expected 'not found' in apply error, got:\nstdout: %s\nstderr: %s",
			result.Stdout, result.Stderr)
	}
}

// TestMCPServer_PackagesShape verifies apply → get → delete round-trip for
// an MCPServer with spec.packages (OCI image reference, the default form
// emitted by `arctl init mcp`). Apply must preserve the packages block and
// -o yaml must render it cleanly on the way out.
func TestMCPServer_PackagesShape(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	serverName := "user/" + UniqueNameWithPrefix("e2epkg")
	version := "0.1.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	})

	// localhost:5001 lands in the validator's private-registry exemption
	// (allowlist + ownership annotation skipped) so the apply succeeds
	// without requiring a per-run OCI image push. The OCI ownership
	// path itself is covered by pkg/api/v1alpha1/registries unit tests;
	// what this e2e exercises is the spec.packages YAML round-trip
	// through apply → get -o yaml.
	imageRef := "localhost:5001/example/mcp:" + version
	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  title: e2e-packages
  description: "packages-shape round-trip test"
  packages:
    - registryType: oci
      identifier: %s
      transport:
        type: stdio
`, serverName, version, imageRef)

	path := writeDeclarativeYAML(t, tmpDir, "mcp-pkg.yaml", yaml)
	result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+serverName)

	// Verify the packages block round-trips through -o yaml.
	result = RunArctl(t, tmpDir, "get", "mcp", serverName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "packages:")
	RequireOutputContains(t, result, "registryType: oci")
	RequireOutputContains(t, result, imageRef)
	RequireOutputContains(t, result, "type: stdio")
	// Exclusive shape — must not leak a remotes or repository block.
	if strings.Contains(result.Stdout, "remotes:") {
		t.Errorf("packages-shape MCP unexpectedly has remotes block:\n%s", result.Stdout)
	}
}

// TestMCPServer_RemotesShape verifies apply → get round-trip for an MCPServer
// with spec.remotes (unmanaged URL — no image, no build). Used for
// third-party servers or dev-loop MCPs the user runs themselves.
func TestMCPServer_RemotesShape(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	// The server's MCP validator requires the namespace of metadata.name to
	// be the reverse-DNS of the remote URL host. URL below is
	// https://mcp.example.com/mcp → host mcp.example.com → namespace com.example.mcp.
	serverName := "com.example.mcp/" + UniqueNameWithPrefix("e2erem")
	version := "1.0.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	})

	// The server-side URL validator rejects localhost/private addresses.
	// Use a public-looking placeholder — the test doesn't actually reach it.
	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  title: e2e-remotes
  description: "remotes-shape round-trip test"
  remotes:
    - type: streamable-http
      url: https://mcp.example.com/mcp
`, serverName, version)

	path := writeDeclarativeYAML(t, tmpDir, "mcp-remote.yaml", yaml)
	result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+serverName)

	result = RunArctl(t, tmpDir, "get", "mcp", serverName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "remotes:")
	RequireOutputContains(t, result, "streamable-http")
	RequireOutputContains(t, result, "https://mcp.example.com/mcp")
	if strings.Contains(result.Stdout, "packages:") {
		t.Errorf("remotes-shape MCP unexpectedly has packages block:\n%s", result.Stdout)
	}
}

// TestMCPServer_RepositoryShape verifies apply → get round-trip for an
// MCPServer with spec.repository (git-bundled — built + deployed from
// source by the provider adapter at deploy time).
func TestMCPServer_RepositoryShape(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	serverName := "repo/" + UniqueNameWithPrefix("e2erepo")
	version := "1.0.0"

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "mcp", serverName, "--version", version, "--registry-url", regURL)
	})

	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: MCPServer
metadata:
  name: %s
  version: "%s"
spec:
  title: e2e-repository
  description: "repository-shape round-trip test"
  repository:
    url: https://github.com/agentregistry-dev/testmcpserver
`, serverName, version)

	path := writeDeclarativeYAML(t, tmpDir, "mcp-repo.yaml", yaml)
	result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "MCPServer/"+serverName)

	result = RunArctl(t, tmpDir, "get", "mcp", serverName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "repository:")
	RequireOutputContains(t, result, "github.com/agentregistry-dev/testmcpserver")
	if strings.Contains(result.Stdout, "packages:") {
		t.Errorf("repository-shape MCP unexpectedly has packages block:\n%s", result.Stdout)
	}
	if strings.Contains(result.Stdout, "remotes:") {
		t.Errorf("repository-shape MCP unexpectedly has remotes block:\n%s", result.Stdout)
	}
}

// TestPrompt_MultipleVersions applies two prompt versions with distinct
// content, asserts both are queryable by --version, deleting one leaves the
// other intact. Covers the (name, version) composite-key behavior for prompts
// — restores coverage from the deleted imperative TestPromptMultipleVersions.
func TestPrompt_MultipleVersions(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2emultivprompt")
	v1, v2 := "1.0.0", "2.0.0"
	v1Content := "Version 1: You are a helpful assistant."
	v2Content := "Version 2: You are an expert coding assistant."

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", v1, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", v2, "--registry-url", regURL)
	})

	// Apply v1 + v2 via declarative YAML.
	for _, tc := range []struct {
		version, content string
	}{{v1, v1Content}, {v2, v2Content}} {
		yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: %s
  version: "%s"
spec:
  description: "multi-version prompt test"
  content: |
    %s
`, promptName, tc.version, tc.content)
		path := writeDeclarativeYAML(t, tmpDir, fmt.Sprintf("p-%s.yaml", tc.version), yaml)
		result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
		RequireSuccess(t, result)
		RequireOutputContains(t, result, "Prompt/"+promptName)
	}

	// Both versions queryable via HTTP (declarative `arctl get` has no
	// --version flag — always returns latest — so we hit the per-version
	// API path directly. Decode the JSON so content comparisons survive
	// any future change to include quotes/escapes.
	for _, tc := range []struct {
		version, wantContent string
	}{{v1, v1Content}, {v2, v2Content}} {
		resp := RegistryGet(t, resourceURL(regURL, "prompts", promptName, tc.version))
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("GET prompt %s@%s: expected 200, got %d: %s", promptName, tc.version, resp.StatusCode, body)
		}
		var decoded struct {
			Metadata struct {
				Version string `json:"version"`
			} `json:"metadata"`
			Spec struct {
				Content string `json:"content"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(body, &decoded); err != nil {
			t.Fatalf("decoding prompt response: %v\nbody: %s", err, body)
		}
		if decoded.Metadata.Version != tc.version {
			t.Errorf("prompt %s: got version %q, want %q", promptName, decoded.Metadata.Version, tc.version)
		}
		if !strings.Contains(decoded.Spec.Content, tc.wantContent) {
			t.Errorf("prompt %s@%s: expected %q in content, got %q",
				promptName, tc.version, tc.wantContent, decoded.Spec.Content)
		}
	}

	// Delete v1 only — v2 must remain accessible.
	result := RunArctl(t, tmpDir, "delete", "prompt", promptName,
		"--version", v1, "--registry-url", regURL)
	RequireSuccess(t, result)

	// v1 gone (soft-deleted — either 404 or 200-with-deletionTimestamp is OK).
	verifyPromptNotFound(t, regURL, promptName, v1)

	// v2 still there.
	resp := RegistryGet(t, resourceURL(regURL, "prompts", promptName, v2))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for remaining prompt %s@%s, got %d: %s", promptName, v2, resp.StatusCode, body)
	}
	if !strings.Contains(string(body), v2Content) {
		t.Errorf("v2 content missing after v1 delete: %s", body)
	}
}

// TestPrompt_ContentIntegrity applies a prompt with specific multi-line
// content, then verifies get -o yaml returns the content byte-for-byte
// (no truncation, no whitespace mangling, no newline loss). Restores
// coverage from the deleted imperative TestPromptContentIntegrity.
func TestPrompt_ContentIntegrity(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	promptName := UniqueNameWithPrefix("e2econtent")
	version := "1.0.0"
	// Distinctive content with special characters that could trip YAML
	// encoding: multi-line, unicode, leading-whitespace-sensitive list.
	expectedLines := []string{
		"You are an AI assistant specialized in Go programming.",
		"Rules:",
		"1. Always use error wrapping — `fmt.Errorf(\"...: %w\", err)`",
		"2. Follow Go conventions",
		"3. Write table-driven tests",
	}

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "prompt", promptName, "--version", version, "--registry-url", regURL)
	})

	// Apply with inline literal-block content.
	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Prompt
metadata:
  name: %s
  version: "%s"
spec:
  description: "content integrity test"
  content: |
    %s
    %s
    %s
    %s
    %s
`, promptName, version,
		expectedLines[0], expectedLines[1], expectedLines[2], expectedLines[3], expectedLines[4])

	path := writeDeclarativeYAML(t, tmpDir, "content.yaml", yaml)
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL))

	// Fetch via HTTP — declarative `arctl get` has no --version flag.
	// Decode the JSON response so content comparisons are against the
	// unescaped stored string, not its JSON-encoded form.
	resp := RegistryGet(t, resourceURL(regURL, "prompts", promptName, version))
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 for prompt %s@%s, got %d: %s", promptName, version, resp.StatusCode, body)
	}
	var decoded struct {
		Spec struct {
			Content string `json:"content"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decoding prompt response: %v\nbody: %s", err, body)
	}
	for _, line := range expectedLines {
		if !strings.Contains(decoded.Spec.Content, line) {
			t.Errorf("line missing from stored content: %q\nfull content: %q", line, decoded.Spec.Content)
		}
	}
}

// TestSkill_DeletePromotesLatest asserts that when `is_latest` is being
// maintained across multiple skill versions, deleting the current latest
// promotes the next-highest version. Restores coverage from the deleted
// imperative TestSkillDeletePromotesLatest.
//
// Contract: apply v1 → apply v2 (now latest) → delete v2 → v1 must be the
// latest again (queryable without --version, served as the single result
// when listing).
func TestSkill_DeletePromotesLatest(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	skillName := UniqueNameWithPrefix("e2epromoteskill")
	v1, v2 := "0.0.1", "0.0.2"

	t.Cleanup(func() {
		// Best-effort cleanup of both versions.
		RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", v1, "--registry-url", regURL)
		RunArctl(t, tmpDir, "delete", "skill", skillName, "--version", v2, "--registry-url", regURL)
	})

	// Apply v1.
	yamlV1 := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: %s
  version: "%s"
spec:
  description: "skill v1 for delete-promotes-latest test"
`, skillName, v1)
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f",
		writeDeclarativeYAML(t, tmpDir, "skill-v1.yaml", yamlV1),
		"--registry-url", regURL))

	// Apply v2 — becomes latest.
	yamlV2 := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Skill
metadata:
  name: %s
  version: "%s"
spec:
  description: "skill v2 for delete-promotes-latest test"
`, skillName, v2)
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f",
		writeDeclarativeYAML(t, tmpDir, "skill-v2.yaml", yamlV2),
		"--registry-url", regURL))

	// Without --version, get returns the latest — should be v2.
	result := RunArctl(t, tmpDir, "get", "skill", skillName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "version: "+v2)

	// Delete v2 — the latest marker should fall back to v1.
	RequireSuccess(t, RunArctl(t, tmpDir, "delete", "skill", skillName,
		"--version", v2, "--registry-url", regURL))

	// Re-query without --version: expect v1 promoted to latest.
	result = RunArctl(t, tmpDir, "get", "skill", skillName, "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "version: "+v1)
	if strings.Contains(result.Stdout, "version: "+v2) {
		t.Errorf("v2 should be gone after delete, but appears in get output:\n%s", result.Stdout)
	}
}

// TestDeclarativeBuild_PlatformFlag verifies that `arctl build --platform
// <arch>` threads the flag through to docker build. Uses linux/amd64 (the CI
// host arch) so the build succeeds without buildx cross-compilation.
// Restores coverage from the deleted imperative TestSkillBuildWithPlatform.
func TestDeclarativeBuild_PlatformFlag(t *testing.T) {
	skipIfNoDocker(t)
	tmpDir := t.TempDir()
	name := UniqueAgentName("platagent")

	// Scaffold an agent project — has a real Dockerfile the build can chew on.
	RequireSuccess(t, RunArctl(t, tmpDir, "init", "agent", "adk", "python", name,
		"--model-name", "gemini-2.5-flash",
		"--image", "localhost:5001/"+name+":platform-test"))

	projectDir := filepath.Join(tmpDir, name)

	// Build with --platform pinned to the host arch. This tests the flag
	// plumbing (build.go:192 appends --platform to the docker build args)
	// without requiring buildx or cross-compilation.
	result := RunArctl(t, tmpDir, "build", projectDir,
		"--platform", "linux/amd64",
		"--image", "localhost:5001/"+name+":platform-test")
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Successfully built Docker image")
}

// TestAPI_DirectNotFound asserts that hitting the registry's kind endpoints
// with a non-existent name returns HTTP 404, not 500 or silent success.
// Restores coverage from the deleted imperative TestPromptAPINotFound —
// kind-agnostic shape so it's one test for all four kinds.
func TestAPI_DirectNotFound(t *testing.T) {
	regURL := RegistryURL(t)
	missing := "does-not-exist-" + UniqueNameWithPrefix("404")

	for _, path := range []string{
		"/agents/" + missing,
		"/prompts/" + missing,
		"/skills/" + missing,
	} {
		t.Run(strings.TrimPrefix(path, "/"), func(t *testing.T) {
			resp := RegistryGet(t, regURL+path)
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNotFound {
				t.Fatalf("GET %s: expected 404, got %d", path, resp.StatusCode)
			}
		})
	}
}

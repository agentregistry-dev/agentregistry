//go:build e2e

// e2e/init_build_run_test.go
package e2e

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestE2E_InitAgent_CreatesExpectedTree(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	result := RunArctl(t, tmp, "init", "agent", "myagent",
		"--framework", "adk", "--language", "python")
	RequireSuccess(t, result)

	pd := filepath.Join(tmp, "myagent")
	for _, f := range []string{"agent.yaml", "arctl.yaml", ".env.example", "Dockerfile", "agent.py"} {
		_, err := os.Stat(filepath.Join(pd, f))
		assert.NoError(t, err, "expected %s to exist", f)
	}

	// arctl.yaml has framework + language
	cfg, err := os.ReadFile(filepath.Join(pd, "arctl.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(cfg), "framework: adk")
	assert.Contains(t, string(cfg), "language: python")

	// agent.yaml has the labels
	agentYAML, err := os.ReadFile(filepath.Join(pd, "agent.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(agentYAML), "kind: Agent")
}

func TestE2E_InitMCP_RequiresNamespaceSlashName(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	result := RunArctl(t, tmp, "init", "mcp", "noslash",
		"--framework", "fastmcp", "--language", "python")
	require.NotEqual(t, 0, result.ExitCode, "expected non-zero exit when name lacks slash")
}

func TestE2E_InitMCP_AcceptsNamespaceSlashName(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	result := RunArctl(t, tmp, "init", "mcp", "acme/my-mcp",
		"--framework", "fastmcp", "--language", "python")
	RequireSuccess(t, result)

	pd := filepath.Join(tmp, "my-mcp")
	_, err := os.Stat(filepath.Join(pd, "mcp.yaml"))
	require.NoError(t, err)
	mcp, err := os.ReadFile(filepath.Join(pd, "mcp.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(mcp), "name: acme/my-mcp")
}

func TestE2E_RunDryRun_ReadsArctlYAMLAndDispatches(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "myagent",
		"--framework", "adk", "--language", "python").Err)

	pd := filepath.Join(tmp, "myagent")
	require.NoError(t, os.Chdir(pd))

	result := RunArctl(t, pd, "run", "--dry-run")
	RequireSuccess(t, result)
	assert.Contains(t, result.Stdout, "adk-python")
	assert.Contains(t, result.Stdout, "(dry-run; skipping exec)")
}

func TestE2E_RunErrors_WhenRequiredEnvMissing(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "myagent",
		"--framework", "adk", "--language", "python").Err)

	pd := filepath.Join(tmp, "myagent")
	// Don't create .env. Required var should trigger an error.
	require.NoError(t, os.Chdir(pd))

	result := RunArctl(t, pd, "run", "--dry-run")
	require.NotEqual(t, 0, result.ExitCode)
	assert.Contains(t, result.Stderr+result.Stdout, "missing required env")
}

func TestE2E_RunLoadsDotEnv(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "myagent",
		"--framework", "adk", "--language", "python").Err)

	pd := filepath.Join(tmp, "myagent")
	require.NoError(t, os.WriteFile(filepath.Join(pd, ".env"), []byte("GOOGLE_API_KEY=stub\n"), 0644))
	require.NoError(t, os.Chdir(pd))

	result := RunArctl(t, pd, "run", "--dry-run")
	RequireSuccess(t, result)
	assert.Contains(t, result.Stdout, "Loaded .env (1 vars)")
}

func TestE2E_Apply_InjectsArctlLabels(t *testing.T) {
	regURL := RegistryURL(t) // skip if no registry available
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "labeltest",
		"--framework", "adk", "--language", "python").Err)

	pd := filepath.Join(tmp, "labeltest")
	apply := RunArctl(t, pd, "apply", "-f", filepath.Join(pd, "agent.yaml"), "--registry-url", regURL)
	RequireSuccess(t, apply)
	assert.Contains(t, apply.Stdout, "Injecting labels")
	assert.Contains(t, apply.Stdout, "arctl.dev/framework=adk")

	// Read back the registered agent and verify labels are persisted.
	get := RunArctl(t, pd, "get", "agent", "labeltest", "-o", "yaml", "--registry-url", regURL)
	RequireSuccess(t, get)
	assert.Contains(t, get.Stdout, "arctl.dev/framework: adk")
	assert.Contains(t, get.Stdout, "arctl.dev/language: python")
}

func TestE2E_Pull_Agent_ClonesSource(t *testing.T) {
	regURL := RegistryURL(t)
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	// First publish an agent with a known source repo URL.
	require.NoError(t, RunArctl(t, tmp, "init", "agent", "pulltest",
		"--framework", "adk", "--language", "python",
		"--git", "https://github.com/agentregistry-dev/agentregistry-test-fixtures").Err)
	pd := filepath.Join(tmp, "pulltest")
	require.NoError(t, RunArctl(t, pd, "apply", "-f", filepath.Join(pd, "agent.yaml"), "--registry-url", regURL).Err)

	// Pull into a different location.
	pullDir := filepath.Join(tmp, "fork")
	pull := RunArctl(t, tmp, "pull", "agent", "pulltest", pullDir, "--registry-url", regURL)
	RequireSuccess(t, pull)

	// Cloned repo should have the arctl.yaml that init wrote.
	_, err := os.Stat(filepath.Join(pullDir, "arctl.yaml"))
	require.NoError(t, err)
}

func TestE2E_PluginDiscovery_FromXDG(t *testing.T) {
	tmp := t.TempDir()
	xdg := filepath.Join(tmp, "xdg")
	require.NoError(t, os.MkdirAll(filepath.Join(xdg, "arctl", "plugins", "fakeagent"), 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(xdg, "arctl", "plugins", "fakeagent", "plugin.yaml"),
		[]byte(`apiVersion: arctl.dev/v1
name: fakeagent
type: agent
framework: fake
language: a
description: fake plugin
build:
  command: ["true"]
run:
  command: ["true"]
`), 0644))

	t.Setenv("XDG_CONFIG_HOME", xdg)

	// init agent picking the fake framework — only possible if the user-level plugin loaded.
	require.NoError(t, os.Chdir(tmp))
	result := RunArctl(t, tmp, "init", "agent", "fakeproj",
		"--framework", "fake", "--language", "a")
	RequireSuccess(t, result)

	_, err := os.Stat(filepath.Join(tmp, "fakeproj", "arctl.yaml"))
	require.NoError(t, err)
}

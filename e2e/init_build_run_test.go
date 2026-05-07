//go:build e2e

// e2e/init_build_run_test.go
package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

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
	for _, f := range []string{"agent.yaml", "arctl.yaml", ".env", ".gitignore", "Dockerfile", "agent/agent.py"} {
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
	// Wipe any required vars from the parent process env so the child
	// arctl invocation actually sees them as missing. CI runners commonly
	// inherit GOOGLE_API_KEY, which would otherwise satisfy the check
	// and turn this into a false negative.
	t.Setenv("GOOGLE_API_KEY", "")

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

	// Use a public agent fixture repo. The registry validates the URL
	// scheme as https, so a hermetic file:// fixture isn't an option.
	const fixtureRepoURL = "https://github.com/agentregistry-dev/testagent"

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "pulltest",
		"--framework", "adk", "--language", "python",
		"--git", fixtureRepoURL).Err)
	pd := filepath.Join(tmp, "pulltest")
	require.NoError(t, RunArctl(t, pd, "apply", "-f", filepath.Join(pd, "agent.yaml"), "--registry-url", regURL).Err)

	// Pull into a different location.
	pullDir := filepath.Join(tmp, "fork")
	pull := RunArctl(t, tmp, "pull", "agent", "pulltest", pullDir, "--registry-url", regURL)
	RequireSuccess(t, pull)

	// Cloned repo should look like an agent project (agent.yaml present).
	_, err := os.Stat(filepath.Join(pullDir, "agent.yaml"))
	require.NoError(t, err)
}

func TestE2E_PluginDiscovery_FromXDG(t *testing.T) {
	tmp := t.TempDir()
	xdg := filepath.Join(tmp, "xdg")
	pluginDir := filepath.Join(xdg, "arctl", "plugins", "fakeagent")
	templatesDir := filepath.Join(pluginDir, "templates")
	require.NoError(t, os.MkdirAll(templatesDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(pluginDir, "plugin.yaml"),
		[]byte(`apiVersion: arctl.dev/v1
name: fakeagent
type: agent
framework: fake
language: a
description: fake plugin
templatesDir: ./templates
build:
  command: ["true"]
run:
  command: ["true"]
`), 0644))
	// Minimal template tree so init's render step succeeds. agent.yaml is
	// re-emitted by the declarative writer, so we only need a stub here to
	// prove the plugin's templates dir is honoured.
	require.NoError(t, os.WriteFile(
		filepath.Join(templatesDir, "agent.yaml.tmpl"),
		[]byte("# stub agent template\nname: {{.Name}}\n"), 0644))

	t.Setenv("XDG_CONFIG_HOME", xdg)

	// init agent picking the fake framework — only possible if the user-level plugin loaded.
	require.NoError(t, os.Chdir(tmp))
	result := RunArctl(t, tmp, "init", "agent", "fakeproj",
		"--framework", "fake", "--language", "a")
	RequireSuccess(t, result)

	_, err := os.Stat(filepath.Join(tmp, "fakeproj", "arctl.yaml"))
	require.NoError(t, err)
}

func TestE2E_RunWatch_RebuildsOnFileChange(t *testing.T) {
	tmp := t.TempDir()
	require.NoError(t, os.Chdir(tmp))

	require.NoError(t, RunArctl(t, tmp, "init", "agent", "watchtest",
		"--framework", "adk", "--language", "python").Err)
	pd := filepath.Join(tmp, "watchtest")
	require.NoError(t, os.WriteFile(filepath.Join(pd, ".env"), []byte("GOOGLE_API_KEY=stub\n"), 0644))

	cmd := exec.Command(arctlBinary(t), "run", "--watch", "--dry-run")
	cmd.Dir = pd
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	require.NoError(t, cmd.Start())
	defer cmd.Process.Kill()

	// Give it a beat, touch a file, expect to see "Change detected".
	time.Sleep(500 * time.Millisecond)
	require.NoError(t, os.WriteFile(filepath.Join(pd, "agent.py"), []byte("# updated"), 0644))

	buf := make([]byte, 4096)
	deadline := time.Now().Add(5 * time.Second)
	got := ""
	for time.Now().Before(deadline) {
		stdout.Read(buf)
		got += string(buf)
		if assert.ObjectsAreEqual(true, contains(got, "Change detected")) {
			return
		}
	}
	t.Fatalf("expected 'Change detected' within 5s; got:\n%s", got)
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}()))
}

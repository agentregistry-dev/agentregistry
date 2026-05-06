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

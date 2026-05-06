package declarative_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/require"
)

func TestRun_DispatchesToPluginRunCommand(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd()
	initCmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "myagent")
	require.NoError(t, os.Chdir(projectDir))

	// Run command should locate arctl.yaml, look up the plugin, and reach
	// plugin.Run.Command. We stop short of actually exec'ing docker by using
	// a NoExec mode (--dry-run) added in this task.
	cmd := declarative.NewRunCmd()
	cmd.SetArgs([]string{"--dry-run"})
	require.NoError(t, cmd.Execute())
}

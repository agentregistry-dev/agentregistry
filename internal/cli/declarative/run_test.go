package declarative_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/require"
)

func TestRun_DispatchesToFrameworkRunCommand(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
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

	// Run command should locate arctl.yaml, look up the framework, and reach
	// framework.Run.Command. We stop short of actually exec'ing docker by using
	// a NoExec mode (--dry-run) added in this task.
	cmd := declarative.NewRunCmd()
	cmd.SetArgs([]string{"--dry-run"})
	require.NoError(t, cmd.Execute())
}

// TestRun_ChatDefault_DryRunNarratesFullLifecycle verifies that for an Agent
// kind, `arctl run --dry-run` reaches the chat-default branch and narrates
// the detached compose-up, readiness wait, chat launch, and teardown without
// shelling out to docker.
func TestRun_ChatDefault_DryRunNarratesFullLifecycle(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd()
	initCmd.SetArgs([]string{"agent", "chatdefault", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "chatdefault")
	require.NoError(t, os.Chdir(projectDir))

	cmd := declarative.NewRunCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--dry-run"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	// Detached compose-up narration.
	require.Contains(t, out, "docker compose")
	require.Contains(t, out, "up -d --build")
	// Readiness wait + chat launch narration.
	require.Contains(t, out, "would wait for http://localhost:8080/")
	require.Contains(t, out, "launch chat (chatdefault)")
	// Teardown narration.
	require.Contains(t, out, "on chat exit would teardown")
	require.Contains(t, out, "down")
	require.Contains(t, out, "(dry-run; skipping exec)")
}

// TestRun_DoesNotRequireAgentYAML proves the structural decoupling: run
// reads arctl.yaml only. Removing agent.yaml from a freshly inited project
// must not break run.
func TestRun_DoesNotRequireAgentYAML(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd()
	initCmd.SetArgs([]string{"agent", "noyaml", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "noyaml")
	require.NoError(t, os.Remove(filepath.Join(projectDir, "agent.yaml")))
	require.NoError(t, os.Chdir(projectDir))

	cmd := declarative.NewRunCmd()
	cmd.SetArgs([]string{"--dry-run"})
	require.NoError(t, cmd.Execute())
}

package declarative_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
)

func TestRun_DispatchesToFrameworkRunCommand(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "myagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "myagent")
	require.NoError(t, os.Chdir(projectDir))

	// Run command should locate arctl.yaml, look up the framework, and reach
	// framework.Run.Command. We stop short of actually exec'ing docker by using
	// a NoExec mode (--dry-run) added in this task.
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
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
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "chatdefault", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "chatdefault")
	require.NoError(t, os.Chdir(projectDir))

	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
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

// TestRun_InspectorOnAgentErrors verifies the strict-symmetric flag
// validation: --inspector is MCP-only and fails fast on agent projects
// before any exec or dry-run narration, with a message pointing the user
// at chat (the agent's equivalent inspection surface).
func TestRun_InspectorOnAgentErrors(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "agentproj", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	require.NoError(t, os.Chdir(filepath.Join(tmp, "agentproj")))
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"--inspector", "--dry-run"})
	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--inspector is only valid for MCP projects")
}

// TestRun_InspectorDryRunNarratesURL verifies that --inspector on an MCP
// project under --dry-run prints the inspector URL the user would see at
// runtime, so docs + CI can assert on the narration without spawning npx.
func TestRun_InspectorDryRunNarratesURL(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"mcp", "acme-inspmcp", "--framework", "fastmcp", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	require.NoError(t, os.Chdir(filepath.Join(tmp, "acme-inspmcp")))
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--inspector", "--dry-run"})
	require.NoError(t, cmd.Execute())

	out := buf.String()
	require.Contains(t, out, "would launch MCP Inspector")
	require.Contains(t, out, "http://localhost:3000/mcp")
	require.Contains(t, out, "(dry-run; skipping exec)")
}

// TestRun_NoChatOnMCPErrors mirrors the above for the other direction:
// --no-chat is agent-only and fails fast on MCP projects.
func TestRun_NoChatOnMCPErrors(t *testing.T) {
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"mcp", "acme-mcpproj", "--framework", "fastmcp", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	require.NoError(t, os.Chdir(filepath.Join(tmp, "acme-mcpproj")))
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"--no-chat", "--dry-run"})
	err = cmd.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "--no-chat is only valid for agent projects")
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
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "noyaml", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	projectDir := filepath.Join(tmp, "noyaml")
	require.NoError(t, os.Remove(filepath.Join(projectDir, "agent.yaml")))
	require.NoError(t, os.Chdir(projectDir))

	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	cmd.SetArgs([]string{"--dry-run"})
	require.NoError(t, cmd.Execute())
}

// TestRun_AgentWatch_DryRunNarratesSignpost verifies that the agent + --watch
// path emits the "where is my agent" + "open chat in another terminal"
// signpost so users know watch is the no-chat foreground iterate mode.
func TestRun_AgentWatch_DryRunNarratesSignpost(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "watchagent", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	require.NoError(t, os.Chdir(filepath.Join(tmp, "watchagent")))
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--watch", "--dry-run"})
	// Pre-cancel so runWithWatch's fsnotify loop exits immediately after
	// printing the signpost + watcher banner. Without this, --watch under
	// dry-run blocks forever waiting on file events.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	require.NoError(t, cmd.Execute())

	out := buf.String()
	require.Contains(t, out, "→ Agent at http://localhost:8080")
	require.Contains(t, out, "→ For chat, open another terminal: arctl run watchagent")
	require.Contains(t, out, "Watching for changes")
}

// TestRun_AgentWatchNoChat_DryRunSuppressesChatHint verifies that when the
// user explicitly opts out of chat with --no-chat alongside --watch, the
// signpost drops the "open another terminal for chat" line — the user
// already said they don't want chat, no need to nag.
func TestRun_AgentWatchNoChat_DryRunSuppressesChatHint(t *testing.T) {
	t.Setenv("GOOGLE_API_KEY", "fake")
	tmp := t.TempDir()
	cwd, err := os.Getwd()
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	require.NoError(t, os.Chdir(tmp))
	initCmd := declarative.NewInitCmd(declarativeTestDeps(nil))
	initCmd.SetArgs([]string{"agent", "watchquiet", "--framework", "adk", "--language", "python"})
	require.NoError(t, initCmd.Execute())

	require.NoError(t, os.Chdir(filepath.Join(tmp, "watchquiet")))
	cmd := declarative.NewRunCmd(declarativeTestDeps(nil))
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--watch", "--no-chat", "--dry-run"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	cmd.SetContext(ctx)
	require.NoError(t, cmd.Execute())

	out := buf.String()
	require.Contains(t, out, "→ Agent at http://localhost:8080")
	require.NotContains(t, out, "For chat, open another terminal")
}

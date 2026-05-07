package declarative

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative/chat"
	"github.com/agentregistry-dev/agentregistry/internal/cli/plugins"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/spf13/cobra"
)

// RunCmd is the cobra command for "run".
// Tests should use NewRunCmd() for a fresh instance.
var RunCmd = newRunCmd()

// NewRunCmd returns a new "run" cobra command.
func NewRunCmd() *cobra.Command {
	return newRunCmd()
}

func newRunCmd() *cobra.Command {
	var (
		extraEnv []string
		dryRun   bool
		watch    bool
		noChat   bool
	)
	cmd := &cobra.Command{
		Use:   "run [DIRECTORY]",
		Short: "Run the agent or MCP server in the current directory",
		Long: `Run the agent or MCP server defined by the declarative YAML in the
project directory (defaults to ".").

For Agents the default is to start the runtime in the background, wait
until the agent's HTTP endpoint is reachable, then launch an interactive
A2A chat. When chat exits the runtime is torn down. Use --no-chat to
keep the old foreground-only behavior.

For MCPServer kinds chat does not apply; the plugin's run command runs
in the foreground until interrupted.

Reads arctl.yaml to determine the (framework, language) plugin and
dispatches to that plugin's run command. Loads .env (if present) and
validates that the plugin's required env vars are set.`,
		Example: `  arctl run
  arctl run ./myagent
  arctl run -e FOO=bar -e BAZ=qux
  arctl run --no-chat
  arctl run --watch`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveProjectDir(args)
			if err != nil {
				return err
			}
			return runProject(cmd.OutOrStdout(), dir, extraEnv, dryRun, watch, noChat)
		},
	}
	cmd.Flags().StringArrayVarP(&extraEnv, "env", "e", nil, "KEY=VALUE env override")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Skip actual exec; useful for tests")
	cmd.Flags().BoolVar(&watch, "watch", false, "Rebuild and restart on file change")
	cmd.Flags().BoolVar(&noChat, "no-chat", false, "Skip chat for Agents; run the plugin command in the foreground")
	return cmd
}

// resolveProjectDir returns an absolute path to the project directory.
// With no args, uses the current working directory; otherwise the first arg.
func resolveProjectDir(args []string) (string, error) {
	dir := "."
	if len(args) == 1 {
		dir = args[0]
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving project directory: %w", err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project directory not found: %s", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("expected a project directory, not a file: %s", abs)
	}
	return abs, nil
}

func runProject(out io.Writer, projectDir string, extraEnv []string, dryRun, watch, noChat bool) error {
	cfg, err := buildconfig.Read(projectDir)
	if err != nil {
		return err
	}

	r, err := loadPluginRegistry(projectDir)
	if err != nil {
		return err
	}

	// Detect kind via sibling envelope.
	obj, yamlFile, err := findDeclarativeResource(projectDir)
	if err != nil {
		return err
	}
	pluginType := "agent"
	switch obj.GetKind() {
	case v1alpha1.KindAgent:
		pluginType = "agent"
	case v1alpha1.KindMCPServer:
		pluginType = "mcp"
	default:
		return fmt.Errorf("kind %q in %s not runnable locally", obj.GetKind(), yamlFile)
	}

	p, ok := r.Lookup(pluginType, cfg.Framework, cfg.Language)
	if !ok {
		return fmt.Errorf("no plugin for %s framework=%s language=%s", pluginType, cfg.Framework, cfg.Language)
	}

	dotEnv, err := LoadDotEnv(projectDir)
	if err != nil {
		return err
	}
	if len(dotEnv) > 0 {
		fmt.Fprintf(out, "→ Loaded .env (%d vars)\n", len(dotEnv))
	}

	if err := ValidateRequiredEnv(dotEnv, p.Env.Required); err != nil {
		return err
	}

	envv := mergeEnv(dotEnv, extraEnv)
	vars := map[string]any{"ProjectDir": projectDir, "PluginDir": p.SourceDir}

	rendered, err := plugins.RenderArgs(p.Run.Command, vars)
	if err != nil {
		return fmt.Errorf("render run command: %w", err)
	}

	// --watch and --dry-run compose: enter the watch loop but skip the
	// actual exec call inside it. This lets tests verify the watcher
	// surface ("Watching for changes…", "Change detected") without
	// shelling out to a long-running runtime.
	if watch {
		return runWithWatch(out, projectDir, p, envv, dryRun)
	}

	// Chat default applies only to Agents (not MCPServers) and when the
	// user hasn't opted out via --no-chat.
	chatMode := pluginType == "agent" && !noChat

	if chatMode {
		return runWithChat(out, projectDir, obj.GetMetadata().Name, p.Name, rendered, envv, dryRun)
	}

	if dryRun {
		fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(rendered, " "))
		fmt.Fprintln(out, "(dry-run; skipping exec)")
		return nil
	}
	fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(rendered, " "))
	return plugins.ExecForeground(p.Run, projectDir, vars, envv)
}

// agentReadinessURL is the URL we poll to know an Agent is ready to chat.
//
// TODO(plugin-contract): hardcoded for adk-python (port 8080 from the
// generated docker-compose, root-path probe). Generalize via a plugin
// descriptor field once a second agent framework lands — e.g.
//
//	plugin.yaml:
//	  run:
//	    readinessURL: "http://localhost:8080/"
//	    teardown: ["docker", "compose", "-f", "{{.ProjectDir}}/docker-compose.yaml", "down"]
const agentReadinessURL = "http://localhost:8080/"

// agentReadinessTimeout caps how long we wait before giving up and tearing down.
const agentReadinessTimeout = 90 * time.Second

// runWithChat starts the runtime in detached mode (compose up -d), polls
// the agent endpoint until it responds, launches the chat TUI, and tears
// down on chat exit. Detached + readiness + chat is the lifecycle
// resurrected from the deleted `arctl agent run`.
//
// dryRun short-circuits: narrate what would happen but don't shell out.
func runWithChat(out io.Writer, projectDir, agentName, pluginName string, rendered, envv []string, dryRun bool) error {
	upArgv := composeUpDetachedArgs(rendered)
	downArgv := composeDownArgs(rendered, projectDir)

	if dryRun {
		fmt.Fprintf(out, "→ %s: %s\n", pluginName, strings.Join(upArgv, " "))
		fmt.Fprintf(out, "→ would wait for %s, then launch chat (%s)\n", agentReadinessURL, agentName)
		fmt.Fprintf(out, "→ on chat exit would teardown: %s\n", strings.Join(downArgv, " "))
		fmt.Fprintln(out, "(dry-run; skipping exec)")
		return nil
	}

	fmt.Fprintf(out, "→ %s: %s\n", pluginName, strings.Join(upArgv, " "))
	upCmd := exec.Command(upArgv[0], upArgv[1:]...)
	upCmd.Dir = projectDir
	upCmd.Stdout = out
	upCmd.Stderr = out
	upCmd.Env = append(os.Environ(), envv...)
	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("docker compose up: %w", err)
	}

	// Always teardown, even if readiness or chat fails. teardown swallows
	// errors past a Fprintln so the original error wins.
	teardown := func() {
		fmt.Fprintln(out, "→ Stopping containers...")
		downCmd := exec.Command(downArgv[0], downArgv[1:]...)
		downCmd.Dir = projectDir
		downCmd.Stdout = out
		downCmd.Stderr = out
		downCmd.Env = append(os.Environ(), envv...)
		if derr := downCmd.Run(); derr != nil {
			fmt.Fprintf(out, "warning: docker compose down failed: %v\n", derr)
		}
	}
	defer teardown()

	// Trap SIGINT/SIGTERM so Ctrl+C during the readiness wait still
	// triggers teardown. Once chat starts, bubbletea owns the terminal
	// and handles Ctrl+C internally; the deferred teardown still runs.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	waitCtx, cancelWait := context.WithCancel(context.Background())
	defer cancelWait()
	go func() {
		select {
		case <-sigCh:
			cancelWait()
		case <-waitCtx.Done():
		}
	}()

	fmt.Fprintf(out, "→ Waiting for agent at %s (timeout %s)...\n", agentReadinessURL, agentReadinessTimeout)
	if err := waitForHTTPReady(waitCtx, agentReadinessURL, agentReadinessTimeout, 1*time.Second, nil); err != nil {
		return fmt.Errorf("agent did not become ready: %w", err)
	}
	fmt.Fprintf(out, "✓ Agent ready at %s\n", agentReadinessURL)

	if err := chat.LaunchA2A(context.Background(), agentName, agentReadinessURL, false); err != nil {
		return fmt.Errorf("chat: %w", err)
	}
	return nil
}

// composeUpDetachedArgs takes a rendered `docker compose ... up` argv and
// returns the same command with `up` replaced by `up -d --build` so it
// returns immediately, leaving containers running for arctl to drive.
//
// If the argv doesn't look like compose-up (no "up" token), it's returned
// unchanged — that's the sign of a non-compose plugin runtime that we
// don't yet know how to drive in chat mode.
func composeUpDetachedArgs(rendered []string) []string {
	out := make([]string, 0, len(rendered)+2)
	replaced := false
	for _, tok := range rendered {
		if !replaced && tok == "up" {
			out = append(out, "up", "-d", "--build")
			replaced = true
			continue
		}
		out = append(out, tok)
	}
	return out
}

// composeDownArgs returns the compose-down command for the project. It
// reuses the rendered up command's structure to find the compose file
// flag so we point `down` at the same compose file. Falls back to
// `docker compose -f <projectDir>/docker-compose.yaml down` if no -f is
// found.
func composeDownArgs(rendered []string, projectDir string) []string {
	args := []string{"docker", "compose"}
	for i := 0; i < len(rendered); i++ {
		if rendered[i] == "-f" && i+1 < len(rendered) {
			args = append(args, "-f", rendered[i+1])
			return append(args, "down")
		}
	}
	args = append(args, "-f", filepath.Join(projectDir, "docker-compose.yaml"), "down")
	return args
}

// mergeEnv flattens dotEnv into KEY=VALUE strings and appends overrides.
//
// Precedence (matches dotenv defaults across Node/Python/Ruby/Go ecosystems):
//   1. --env CLI flags (highest, in `overrides`)
//   2. Process env (the user's shell export)
//   3. .env file (project default)
//
// Empty .env values are skipped — they are unfilled placeholders written
// by `arctl init`. .env entries whose key already has a non-empty value
// in process env are also skipped, so the user's shell export wins.
// Overrides come last so explicit --env flags trump everything.
func mergeEnv(dotEnv map[string]string, overrides []string) []string {
	out := make([]string, 0, len(dotEnv)+len(overrides))
	for k, v := range dotEnv {
		if v == "" {
			continue
		}
		if existing := os.Getenv(k); existing != "" {
			continue // process env wins over .env file
		}
		out = append(out, k+"="+v)
	}
	out = append(out, overrides...)
	return out
}

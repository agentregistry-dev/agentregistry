package declarative

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
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
	)
	cmd := &cobra.Command{
		Use:   "run [DIRECTORY]",
		Short: "Run the agent or MCP server in the current directory",
		Long: `Run the agent or MCP server defined by the declarative YAML in the
project directory (defaults to ".").

Reads arctl.yaml to determine the (framework, language) plugin and
dispatches to that plugin's run command. Loads .env (if present) and
validates that the plugin's required env vars are set.`,
		Example: `  arctl run
  arctl run ./myagent
  arctl run -e FOO=bar -e BAZ=qux
  arctl run --watch`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := resolveProjectDir(args)
			if err != nil {
				return err
			}
			return runProject(cmd.OutOrStdout(), dir, extraEnv, dryRun, watch)
		},
	}
	cmd.Flags().StringArrayVarP(&extraEnv, "env", "e", nil, "KEY=VALUE env override")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Skip actual exec; useful for tests")
	cmd.Flags().BoolVar(&watch, "watch", false, "Rebuild and restart on file change")
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

func runProject(out io.Writer, projectDir string, extraEnv []string, dryRun, watch bool) error {
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
	if dryRun {
		fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(rendered, " "))
		fmt.Fprintln(out, "(dry-run; skipping exec)")
		return nil
	}
	fmt.Fprintf(out, "→ %s: %s\n", p.Name, strings.Join(rendered, " "))
	return plugins.ExecForeground(p.Run, projectDir, vars, envv)
}

// mergeEnv flattens dotEnv into KEY=VALUE strings and appends overrides.
// Overrides come last, so the child process sees them as the effective value.
func mergeEnv(dotEnv map[string]string, overrides []string) []string {
	out := make([]string, 0, len(dotEnv)+len(overrides))
	for k, v := range dotEnv {
		out = append(out, k+"="+v)
	}
	out = append(out, overrides...)
	return out
}

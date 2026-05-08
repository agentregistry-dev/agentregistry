package declarative

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/frameworks"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	skilltemplates "github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/validators"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// InitCmd is the cobra command for "init".
// Tests should use NewInitCmd() for a fresh instance.
var InitCmd = newInitCmd()

// lookupOutputDir walks the cmd → parent chain to find the --output-dir
// flag value. The flag is defined as persistent on the parent `init`
// command; cobra normally merges it into child flag sets at Execute time,
// but the kindless dispatch (top-level RunE → child.RunE directly) skips
// that merge, so we walk explicitly.
func lookupOutputDir(cmd *cobra.Command) string {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.Flags().Lookup("output-dir"); f != nil {
			return f.Value.String()
		}
		if f := c.PersistentFlags().Lookup("output-dir"); f != nil {
			return f.Value.String()
		}
	}
	return ""
}

// resolveInitProjectPath returns the absolute path the new project should
// occupy. If `--output-dir` is set on the parent init command, the project
// goes under that directory; otherwise it falls under cwd. Either way the
// path is made absolute so downstream code doesn't have to re-resolve.
func resolveInitProjectPath(cmd *cobra.Command, projectName string) (string, error) {
	outputDir := lookupOutputDir(cmd)
	if outputDir != "" {
		base, err := filepath.Abs(outputDir)
		if err != nil {
			return "", fmt.Errorf("resolving output-dir: %w", err)
		}
		return filepath.Join(base, projectName), nil
	}
	abs, err := filepath.Abs(projectName)
	if err != nil {
		return "", fmt.Errorf("resolving project dir: %w", err)
	}
	return abs, nil
}

// displayPath returns the path the user sees in narration. When projectDir
// is under cwd, returns a short relative path (`outdirbot`). When elsewhere
// (e.g. via --output-dir to a sibling tree), returns the absolute path.
func displayPath(projectDir string) string {
	cwd, err := os.Getwd()
	if err != nil {
		return projectDir
	}
	rel, err := filepath.Rel(cwd, projectDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return projectDir
	}
	return rel
}

// NewInitCmd returns a new "init" cobra command.
func NewInitCmd() *cobra.Command {
	return newInitCmd()
}

func newInitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "init TYPE ...",
		Short: "Scaffold a new resource project with declarative YAML",
		Long: `Scaffold a new project. The generated YAML uses the ar.dev/v1alpha1
declarative format and can be applied directly with 'arctl apply'.

Supported types:
  agent NAME              # picker selects framework + language
  mcp NAMESPACE/NAME      # picker selects framework + language
  skill NAME
  prompt NAME

Examples:
  arctl init agent myagent
  arctl init agent myagent --framework adk --language python
  arctl init mcp acme/my-server
  arctl init mcp acme/my-server --framework fastmcp --language python
  arctl init skill my-skill
  arctl init prompt my-prompt
  arctl init                                    # interactive: picker for kind`,
		SilenceUsage: true,
		Args:         cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// Kindless wizard: pick a resource type, then dispatch into that
			// subcommand's RunE with no args (which fires its own name prompt
			// and any other interactive flows).
			if !isatty() {
				return cmd.Help()
			}
			kind, err := runInitTypePicker()
			if err != nil {
				return err
			}
			for _, c := range cmd.Commands() {
				if c.Name() == kind {
					return c.RunE(c, []string{})
				}
			}
			return fmt.Errorf("internal: no subcommand for kind %q", kind)
		},
	}
	cmd.PersistentFlags().String("output-dir", "", "Parent directory under which the project is created. Defaults to the current directory.")
	cmd.AddCommand(newInitAgentCmd())
	cmd.AddCommand(newInitMCPCmd())
	cmd.AddCommand(newInitSkillCmd())
	cmd.AddCommand(newInitPromptCmd())

	// init is an offline scaffolding command — hide inherited registry flags
	// from --help output. Subcommands inherit the help func from the parent.
	common.HideRegistryFlags(cmd)
	return cmd
}

func newInitAgentCmd() *cobra.Command {
	var (
		initVersion       string
		initDescription   string
		initModelProvider string
		initModelName     string
		initFramework     string
		initLanguage      string
		initImage         string
		initGit           string
		initMCPs          []string
		initLocalMCPs     []string
	)

	cmd := &cobra.Command{
		Use:   "agent NAME",
		Short: "Scaffold a new agent project",
		Long: `Scaffold a new agent project.

Picks a framework + language interactively (or via --framework / --language).
Writes:
  - agent.yaml — v1alpha1 envelope
  - arctl.yaml — local build config (framework + language)
  - .env — env vars the chosen framework needs (gitignored)

To wire a sibling arctl-init'd MCP project for local dev, pass --local-mcp.
For an MCP at an arbitrary URL (remote, or local-not-arctl), edit .env after
init and add an MCP_SERVERS_CONFIG entry, e.g.:

  MCP_SERVERS_CONFIG=[{"name":"my-remote","type":"remote","url":"https://mcp.example.com/sse"}]`,
		Example: `  arctl init agent myagent
  arctl init agent myagent --framework adk --language python
  arctl init agent myagent --local-mcp ../my-mcp`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
				if err := validators.ValidateAgentName(name); err != nil {
					return err
				}
			} else {
				typed, err := promptText("Project name", "myagent",
					func(s string) error { return validators.ValidateAgentName(s) },
					cmd.OutOrStdout(), cmd.InOrStdin())
				if err != nil {
					return err
				}
				name = typed
			}

			projectDir, err := resolveInitProjectPath(cmd, name)
			if err != nil {
				return err
			}

			if err := handleExistingProjectDir(projectDir, cmd.OutOrStdout(), cmd.InOrStdin()); err != nil {
				if errors.Is(err, errOverwriteHandled) {
					return nil
				}
				return err
			}

			r, err := loadFrameworkRegistry(projectDir)
			if err != nil {
				return err
			}
			framework, err := frameworks.Pick(frameworks.PickOpts{
				Registry:       r,
				Type:           "agent",
				Framework:      initFramework,
				Language:       initLanguage,
				NonInteractive: !isatty(),
			})
			if err != nil {
				return err
			}

			image := initImage
			if image == "" {
				registry := strings.TrimSuffix(version.DockerRegistry, "/")
				if registry == "" {
					registry = "localhost:5001"
				}
				image = fmt.Sprintf("%s/%s:latest", registry, name)
			}

			// Resolve provider + model name once, then thread the resolved
			// values into templates, arctl.yaml, and agent.yaml so all three
			// agree. Provider comes from --model-provider flag; otherwise the
			// interactive picker if a TTY is available; otherwise "gemini".
			// User-cancel propagates as an error; TTY-unavailable falls back
			// silently so tests and headless runs continue to work.
			provider := initModelProvider
			if provider == "" && isatty() {
				picked, perr := runModelProviderPicker()
				if errors.Is(perr, errProviderPickCancelled) {
					return perr
				}
				if perr == nil {
					provider = picked
				}
			}
			if provider == "" {
				provider = "gemini"
			}
			modelName := initModelName
			if modelName == "" {
				modelName = defaultInitModelName(provider)
				if isatty() {
					typed, perr := promptText("Model name", modelName, nil, cmd.OutOrStdout(), cmd.InOrStdin())
					if perr == nil {
						modelName = typed
					}
				}
			}

			vars := agentTemplateVars(name, initVersion, initDescription, provider, modelName, image, framework.SourceDir, projectDir)
			if err := frameworks.RenderTemplates(framework, projectDir, vars); err != nil {
				return fmt.Errorf("render templates: %w", err)
			}

			cfg := &buildconfig.Config{
				Framework:     framework.Framework,
				Language:      framework.Language,
				ModelProvider: provider,
				ModelName:     modelName,
			}
			if err := buildconfig.Write(projectDir, cfg); err != nil {
				return fmt.Errorf("write arctl.yaml: %w", err)
			}

			// Required env = framework's infra keys + model provider's keys.
			// arctl owns the provider→keys map (see modelenv.go) so frameworks
			// don't have to restate it.
			required := append([]string{}, framework.Env.Required...)
			required = append(required, ModelProviderEnvKeys(provider)...)

			if err := buildconfig.WriteDotEnv(projectDir, required, framework.Env.Optional); err != nil {
				return fmt.Errorf("write .env: %w", err)
			}
			if len(required) > 0 || len(framework.Env.Optional) > 0 {
				if err := buildconfig.EnsureGitignored(projectDir, ".env"); err != nil {
					return fmt.Errorf("update .gitignore: %w", err)
				}
			}

			// --local-mcp wires sibling MCP projects via the runtime's
			// MCP_SERVERS_CONFIG env var. Each path's arctl.yaml carries
			// the port; we write a JSON array of {name, url} entries.
			if len(initLocalMCPs) > 0 {
				if err := appendLocalMCPsToDotEnv(projectDir, initLocalMCPs); err != nil {
					return fmt.Errorf("wire local MCPs: %w", err)
				}
			}

			// Skills/Prompts/Language/Framework removed from AgentSpec (Phase 11);
			// language/framework now live in arctl.yaml only. The declarative
			// agent.yaml carries only canonical AgentSpec fields.
			if err := writeDeclarativeAgentYAML(projectDir, name, initVersion, image,
				provider, modelName,
				initDescription, initGit, initMCPs); err != nil {
				return fmt.Errorf("write agent.yaml: %w", err)
			}

			disp := displayPath(projectDir)
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created agent: %s (framework: %s, language: %s, model: %s/%s)\n", name, framework.Framework, framework.Language, provider, modelName)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. Run locally (optional):\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl run %s\n", disp)
			if len(required) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "     (export %s in your shell or set it in .env first)\n", strings.Join(required, ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  2. Publish to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f %s/agent.yaml\n", disp)
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "Agent description")
	cmd.Flags().StringVar(&initFramework, "framework", "", "Framework (e.g. adk). Skips picker.")
	cmd.Flags().StringVar(&initLanguage, "language", "", "Language (e.g. python). Skips picker.")
	cmd.Flags().StringVar(&initModelProvider, "model-provider", "", "Model provider")
	cmd.Flags().StringVar(&initModelName, "model-name", "", "Model name")
	cmd.Flags().StringVar(&initImage, "image", "", "Image tag override")
	cmd.Flags().StringVar(&initGit, "git", "", "Git repository URL")
	cmd.Flags().StringSliceVar(&initMCPs, "mcp", nil, "Registry MCP server ref (name@version). Repeatable.")
	cmd.Flags().StringSliceVar(&initLocalMCPs, "local-mcp", nil, "Path to a sibling MCP project; wires it into .env so the local agent can reach it. Repeatable.")
	return cmd
}

// appendLocalMCPsToDotEnv resolves each sibling MCP project path, reads its
// arctl.yaml for the port, derives the registry-style name from the sibling's
// mcp.yaml, and appends a single MCP_SERVERS_CONFIG line to projectDir/.env
// carrying a JSON array of {name, url} entries.
//
// host.docker.internal works on Docker Desktop (Mac/Windows) by default. Linux
// users need `--add-host=host.docker.internal:host-gateway` in the agent's
// docker-compose; we don't auto-inject that here.
func appendLocalMCPsToDotEnv(projectDir string, paths []string) error {
	// The kagent ADK runtime's mcp_tools.py reads this JSON and dispatches on
	// `type`: "command" for in-cluster sidecar services (URL derived from
	// service name) vs everything else (uses `url` directly). For local-dev
	// wiring we always emit "remote" so the runtime takes the URL path.
	type entry struct {
		Name string `json:"name"`
		Type string `json:"type"`
		URL  string `json:"url"`
	}
	var entries []entry
	for _, p := range paths {
		abs, err := filepath.Abs(p)
		if err != nil {
			return fmt.Errorf("resolve %q: %w", p, err)
		}
		cfg, err := buildconfig.Read(abs)
		if err != nil {
			return fmt.Errorf("read sibling arctl.yaml at %s: %w", abs, err)
		}
		port := cfg.Port
		if port == 0 {
			port = 3000
		}

		// Sibling MCP's registry-style name (e.g. "acme/foo") lives in mcp.yaml.
		siblingName, err := readMCPName(abs)
		if err != nil {
			return err
		}
		entries = append(entries, entry{
			Name: siblingName,
			Type: "remote",
			URL:  fmt.Sprintf("http://host.docker.internal:%d/mcp", port),
		})
	}

	jsonBlob, err := json.Marshal(entries)
	if err != nil {
		return fmt.Errorf("marshal MCP_SERVERS_CONFIG: %w", err)
	}
	envPath := filepath.Join(projectDir, ".env")
	existing, err := os.ReadFile(envPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	out := string(existing)
	if out != "" && !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	out += "\n# Wired by arctl init --local-mcp.\n"
	out += "MCP_SERVERS_CONFIG=" + string(jsonBlob) + "\n"
	return os.WriteFile(envPath, []byte(out), 0o644)
}

// readMCPName pulls metadata.name out of a sibling mcp.yaml. Used to label
// entries in MCP_SERVERS_CONFIG.
func readMCPName(projectDir string) (string, error) {
	data, err := os.ReadFile(filepath.Join(projectDir, "mcp.yaml"))
	if err != nil {
		return "", fmt.Errorf("read sibling mcp.yaml: %w", err)
	}
	var env struct {
		Metadata struct {
			Name string `yaml:"name"`
		} `yaml:"metadata"`
	}
	if err := yaml.Unmarshal(data, &env); err != nil {
		return "", fmt.Errorf("parse sibling mcp.yaml: %w", err)
	}
	if env.Metadata.Name == "" {
		return "", fmt.Errorf("sibling mcp.yaml missing metadata.name")
	}
	return env.Metadata.Name, nil
}

// defaultInitModelName returns the default model name for a provider when
// the user doesn't pass --model-name. Empty result means no default — the
// caller leaves modelName blank and the user must fill it in later.
func defaultInitModelName(provider string) string {
	switch strings.ToLower(provider) {
	case "openai", "agentgateway":
		return "gpt-5-mini"
	case "anthropic":
		return "claude-sonnet-4-6"
	case "gemini":
		return "gemini-2.5-flash"
	case "bedrock":
		return "anthropic.claude-haiku-4-5-20251001-v1:0"
	case "azureopenai":
		return "your-deployment-name"
	default:
		return ""
	}
}

// agentTemplateVars returns the template-substitution vars for the agent
// framework's templates. The in-tree adk-python templates (vendored from the
// legacy generator) reference fields beyond the canonical Phase-5 set,
// so we provide safe defaults for those here. Phase 12 simplifies the
// templates and trims this to the canonical set.
func agentTemplateVars(name, version, description, modelProvider, modelName, image, frameworkDir, projectDir string) map[string]any {
	mp := strings.ToLower(modelProvider)
	mn := modelName
	return map[string]any{
		"Name":                  name,
		"Version":               version,
		"Description":           description,
		"ModelProvider":         mp,
		"ModelName":             mn,
		"Image":                 image,
		"FrameworkDir":             frameworkDir,
		"ProjectDir":            projectDir,
		"Instruction":           "",
		"KagentADKImageVersion": "0.8.0-beta6",
		"KagentADKPyVersion":    "0.8.0b6",
		"Port":                  8080,
		"EnvVars":               []string{},
		"McpServers":            []struct{}{},
		"HasSkills":             false,
		"Targets":               []struct{}{},
	}
}

// loadFrameworkRegistry centralizes the standard load order for arctl commands.
func loadFrameworkRegistry(projectRoot string) (*frameworks.Registry, error) {
	stage, err := os.MkdirTemp("", "arctl-frameworks-*")
	if err != nil {
		return nil, err
	}
	return frameworks.LoadAll(frameworks.LoadOpts{
		StageDir:    stage,
		UserDir:     frameworks.UserFrameworksDir(),
		ProjectRoot: projectRoot,
	})
}

func isatty() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// parseNameVersion splits "name@version" into (name, version).
// If no @ is present, version defaults to "latest".
// If the name part is empty (e.g. "@1.0.0"), the whole string is treated as the name.
func parseNameVersion(s string) (string, string) {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i], s[i+1:]
	}
	return s, "latest"
}

// writeDeclarativeAgentYAML writes agent.yaml in the ar.dev/v1alpha1 declarative format.
func writeDeclarativeAgentYAML(projectDir, name, ver, image, modelProvider, modelName, description, gitURL string, mcps []string) error {
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s agent", name)
	}

	agent := v1alpha1.Agent{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: scheme.APIVersion,
			Kind:       v1alpha1.KindAgent,
		},
		Metadata: v1alpha1.ObjectMeta{
			Name:    name,
			Version: ver,
		},
		Spec: v1alpha1.AgentSpec{
			ModelProvider: modelProvider,
			ModelName:     modelName,
			Description:   desc,
			Source: &v1alpha1.AgentSource{
				Image: image,
			},
		},
	}

	if gitURL != "" {
		agent.Spec.Source.Repository = &v1alpha1.Repository{
			URL: gitURL,
		}
	}

	for _, raw := range mcps {
		serverName, mcpVer := parseNameVersion(raw)
		agent.Spec.MCPServers = append(agent.Spec.MCPServers, v1alpha1.ResourceRef{
			Kind:    v1alpha1.KindMCPServer,
			Name:    serverName,
			Version: mcpVer,
		})
	}

	b, err := yaml.Marshal(agent)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "agent.yaml"), b, 0o644)
}

// --- init mcp ---

func newInitMCPCmd() *cobra.Command {
	var (
		initVersion     string
		initDescription string
		initImage       string
		initFramework   string
		initLanguage    string
		initPort        int
	)

	cmd := &cobra.Command{
		Use:   "mcp NAMESPACE/NAME",
		Short: "Scaffold a new MCP server project",
		Long: `Scaffold a new MCP server project.

NAME must be in namespace/name format (registry requirement).
Picks a framework + language interactively (or via --framework / --language).`,
		Example: `  arctl init mcp acme/my-mcp
  arctl init mcp acme/my-mcp --framework fastmcp --language python`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var full string
			if len(args) == 1 {
				full = args[0]
			} else {
				typed, err := promptText("Project name", "myorg/mymcp",
					func(s string) error {
						parts := strings.SplitN(s, "/", 2)
						if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
							return fmt.Errorf("name must be in namespace/name format")
						}
						return nil
					},
					cmd.OutOrStdout(), cmd.InOrStdin())
				if err != nil {
					return err
				}
				full = typed
			}
			parts := strings.SplitN(full, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return fmt.Errorf("name must be in namespace/name format (got %q)", full)
			}
			projectName := parts[1]

			projectDir, err := resolveInitProjectPath(cmd, projectName)
			if err != nil {
				return err
			}

			if err := handleExistingProjectDir(projectDir, cmd.OutOrStdout(), cmd.InOrStdin()); err != nil {
				if errors.Is(err, errOverwriteHandled) {
					return nil
				}
				return err
			}

			r, err := loadFrameworkRegistry(projectDir)
			if err != nil {
				return err
			}
			framework, err := frameworks.Pick(frameworks.PickOpts{
				Registry: r, Type: "mcp",
				Framework: initFramework, Language: initLanguage,
				NonInteractive: !isatty(),
			})
			if err != nil {
				return err
			}

			image := initImage
			if image == "" {
				registry := strings.TrimSuffix(version.DockerRegistry, "/")
				if registry == "" {
					registry = "localhost:5001"
				}
				image = fmt.Sprintf("%s/%s:latest", registry, projectName)
			}

			vars := mcpTemplateVars(full, projectName, initVersion, initDescription, image, framework.SourceDir, projectDir)
			if err := frameworks.RenderTemplates(framework, projectDir, vars); err != nil {
				return err
			}
			if err := buildconfig.Write(projectDir, &buildconfig.Config{
				Framework: framework.Framework,
				Language:  framework.Language,
				Port:      initPort,
			}); err != nil {
				return err
			}
			if err := buildconfig.WriteDotEnv(projectDir, framework.Env.Required, framework.Env.Optional); err != nil {
				return err
			}
			if len(framework.Env.Required) > 0 || len(framework.Env.Optional) > 0 {
				if err := buildconfig.EnsureGitignored(projectDir, ".env"); err != nil {
					return fmt.Errorf("update .gitignore: %w", err)
				}
			}
			if err := writeDeclarativeMCPYAML(projectDir, full, initVersion, image, initDescription); err != nil {
				return err
			}

			disp := displayPath(projectDir)
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created MCP server: %s (framework: %s, language: %s, port: %d)\n", full, framework.Framework, framework.Language, initPort)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. Run locally (optional):\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl run %s\n", disp)
			if len(framework.Env.Required) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "     (export %s in your shell or set it in .env first)\n", strings.Join(framework.Env.Required, ", "))
			}
			fmt.Fprintf(cmd.OutOrStdout(), "  2. Publish to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f %s/mcp.yaml\n", disp)
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "MCP server description")
	cmd.Flags().StringVar(&initImage, "image", "", "Image tag override")
	cmd.Flags().StringVar(&initFramework, "framework", "", "Framework. Skips picker.")
	cmd.Flags().StringVar(&initLanguage, "language", "", "Language. Skips picker.")
	cmd.Flags().IntVar(&initPort, "port", 3000, "HTTP port the MCP server binds to (and that arctl run maps)")
	return cmd
}

// mcpTemplateVars returns the template-substitution vars for the mcp framework's
// templates. The vendored fastmcp-python and mcp-go templates reference fields
// beyond the canonical Phase-5 set, so we supply safe defaults for those here.
// Phase 12 simplifies the templates and trims this.
func mcpTemplateVars(name, baseName, version, description, image, frameworkDir, projectDir string) map[string]any {
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s MCP server", baseName)
	}
	toolName := "echo"
	return map[string]any{
		"Name":          name,
		"BaseName":      baseName,
		"Version":       version,
		"Description":   desc,
		"description":   desc, // legacy lowercase alias used by some templates
		"Image":         image,
		"FrameworkDir":     frameworkDir,
		"ProjectDir":    projectDir,
		"ProjectName":   baseName,
		"ToolName":      toolName,
		"ToolNameTitle": "Echo",
		"ClassName":     "Server",
		"GoModuleName":  "github.com/example/" + baseName,
		"Author":        "",
		"Email":         "",
	}
}

func writeDeclarativeMCPYAML(projectDir, name, ver, image, description string) error {
	nameParts := strings.SplitN(name, "/", 2)
	shortName := nameParts[len(nameParts)-1]

	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s MCP server", shortName)
	}

	server := v1alpha1.MCPServer{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: scheme.APIVersion,
			Kind:       v1alpha1.KindMCPServer,
		},
		Metadata: v1alpha1.ObjectMeta{
			Name:    name,
			Version: ver,
		},
		Spec: v1alpha1.MCPServerSpec{
			Title:       shortName,
			Description: desc,
			Source: &v1alpha1.MCPServerSource{
				Package: &v1alpha1.MCPPackage{
					RegistryType: "oci",
					Identifier:   image,
					Transport:    v1alpha1.MCPTransport{Type: "stdio"},
				},
			},
		},
	}

	b, err := yaml.Marshal(server)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "mcp.yaml"), b, 0o644)
}

// --- init skill ---

func newInitSkillCmd() *cobra.Command {
	var (
		initVersion     string
		initDescription string
	)

	cmd := &cobra.Command{
		Use:   "skill NAME",
		Short: "Scaffold a new skill project with declarative skill.yaml",
		Long: `Scaffold a new skill project. Creates a project directory
containing a declarative skill.yaml (ar.dev/v1alpha1) and source stubs.

The generated skill.yaml can be applied directly:
  arctl apply -f NAME/skill.yaml`,
		Example: `  arctl init skill my-skill
  arctl init skill my-skill --description "Text summarizer"`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			} else {
				typed, err := promptText("Project name", "myskill",
					func(s string) error { return validators.ValidateSkillName(s) },
					cmd.OutOrStdout(), cmd.InOrStdin())
				if err != nil {
					return err
				}
				name = typed
			}

			if err := validators.ValidateSkillName(name); err != nil {
				return fmt.Errorf("invalid skill name: %w", err)
			}

			projectDir, err := resolveInitProjectPath(cmd, name)
			if err != nil {
				return err
			}

			if err := handleExistingProjectDir(projectDir, cmd.OutOrStdout(), cmd.InOrStdin()); err != nil {
				if errors.Is(err, errOverwriteHandled) {
					return nil
				}
				return err
			}

			if err := skilltemplates.NewGenerator().GenerateProject(skilltemplates.ProjectConfig{
				ProjectName: name,
				Directory:   projectDir,
				NoGit:       false,
			}); err != nil {
				return fmt.Errorf("generating skill project: %w", err)
			}

			if err := writeDeclarativeSkillYAML(projectDir, name, initVersion, initDescription); err != nil {
				return fmt.Errorf("writing declarative skill.yaml: %w", err)
			}

			disp := displayPath(projectDir)
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created skill: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. Edit %s/SKILL.md and references/ (optional)\n", disp)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. Publish to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f %s/skill.yaml\n", disp)
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "Skill description")

	return cmd
}

func writeDeclarativeSkillYAML(projectDir, name, ver, description string) error {
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s skill", name)
	}

	skill := v1alpha1.Skill{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: scheme.APIVersion,
			Kind:       v1alpha1.KindSkill,
		},
		Metadata: v1alpha1.ObjectMeta{
			Name:    name,
			Version: ver,
		},
		Spec: v1alpha1.SkillSpec{
			Title:       name,
			Description: desc,
		},
	}

	b, err := yaml.Marshal(skill)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "skill.yaml"), b, 0o644)
}

// --- init prompt ---

func newInitPromptCmd() *cobra.Command {
	var (
		initVersion     string
		initDescription string
		initContent     string
	)

	cmd := &cobra.Command{
		Use:   "prompt NAME",
		Short: "Create a new declarative <name>.yaml for a prompt",
		Long: `Create a new <name>.yaml in the current directory using the
ar.dev/v1alpha1 declarative format. No code scaffolding is generated.

The generated file can be applied directly:
  arctl apply -f my-prompt.yaml`,
		Example: `  arctl init prompt my-prompt
  arctl init prompt my-prompt --description "System prompt for summarization"`,
		Args:         cobra.MaximumNArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			var name string
			if len(args) == 1 {
				name = args[0]
			} else {
				typed, err := promptText("Project name", "myprompt",
					func(s string) error { return validators.ValidateSkillName(s) },
					cmd.OutOrStdout(), cmd.InOrStdin())
				if err != nil {
					return err
				}
				name = typed
			}

			// Prompt names follow the same DB constraint as skill names (^[a-zA-Z0-9_-]+$).
			if err := validators.ValidateSkillName(name); err != nil {
				return fmt.Errorf("invalid prompt name: %w", err)
			}

			// Content is the prompt's payload — prompt for it interactively
			// when --content not supplied and a TTY is available.
			if !cmd.Flags().Changed("content") && isatty() {
				typed, perr := promptText("Content", initContent, nil, cmd.OutOrStdout(), cmd.InOrStdin())
				if perr == nil {
					initContent = typed
				}
			}

			// Prompts are a single YAML file — no project directory. --output-dir
			// (when set) becomes the parent dir for the file. lookupOutputDir
			// walks the parent chain so the kindless dispatch sees it.
			outputDir := lookupOutputDir(cmd)
			parent, err := filepath.Abs(outputDir) // "" → cwd
			if err != nil {
				return fmt.Errorf("resolving output-dir: %w", err)
			}
			if outputDir != "" {
				if err := os.MkdirAll(parent, 0o755); err != nil {
					return fmt.Errorf("creating output dir: %w", err)
				}
			}
			outPath := filepath.Join(parent, name+".yaml")

			if err := handleExistingFile(outPath, cmd.OutOrStdout(), cmd.InOrStdin()); err != nil {
				if errors.Is(err, errOverwriteHandled) {
					return nil
				}
				return err
			}

			if err := writeDeclarativePromptYAML(outPath, name, initVersion, initDescription, initContent); err != nil {
				return fmt.Errorf("writing declarative prompt.yaml: %w", err)
			}

			disp := displayPath(outPath)
			fmt.Fprintf(cmd.OutOrStdout(), "✓ Created prompt: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. Edit %s (optional)\n", disp)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. Publish to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f %s\n", disp)
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "Prompt description")
	cmd.Flags().StringVar(&initContent, "content", "You are a helpful assistant.", "Initial prompt content")

	return cmd
}

func writeDeclarativePromptYAML(path, name, ver, description, content string) error {
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s prompt", name)
	}

	prompt := v1alpha1.Prompt{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: scheme.APIVersion,
			Kind:       v1alpha1.KindPrompt,
		},
		Metadata: v1alpha1.ObjectMeta{
			Name:    name,
			Version: ver,
		},
		Spec: v1alpha1.PromptSpec{
			Description: desc,
			Content:     content,
		},
	}

	b, err := yaml.Marshal(prompt)
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0o644)
}

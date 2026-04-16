package declarative

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agentframeworks "github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks"
	agentcommon "github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	mcpframeworks "github.com/agentregistry-dev/agentregistry/internal/cli/mcp/frameworks"
	mcptemplates "github.com/agentregistry-dev/agentregistry/internal/cli/mcp/templates"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	skilltemplates "github.com/agentregistry-dev/agentregistry/internal/cli/skill/templates"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/validators"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// InitCmd is the cobra command for "init".
// Tests should use NewInitCmd() for a fresh instance.
var InitCmd = newInitCmd()

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
  agent FRAMEWORK LANGUAGE NAME
  mcp   FRAMEWORK NAME
  skill NAME
  prompt NAME

Examples:
  arctl init agent adk python myagent
  arctl init mcp fastmcp-python myorg/my-server
  arctl init skill my-skill
  arctl init prompt my-prompt`,
		SilenceUsage: true,
	}
	cmd.AddCommand(newInitAgentCmd())
	cmd.AddCommand(newInitMCPCmd())
	cmd.AddCommand(newInitSkillCmd())
	cmd.AddCommand(newInitPromptCmd())
	return cmd
}

func newInitAgentCmd() *cobra.Command {
	var (
		initVersion       string
		initDescription   string
		initModelProvider string
		initModelName     string
		initImage         string
		initGit           string
		initMCPs          []string
		initSkills        []string
		initPrompts       []string
	)

	cmd := &cobra.Command{
		Use:   "agent FRAMEWORK LANGUAGE NAME",
		Short: "Scaffold a new agent project with declarative agent.yaml",
		Long: `Scaffold a new ADK Python agent project. Creates a project directory
containing a declarative agent.yaml (ar.dev/v1alpha1), Dockerfile, and source stubs.

The generated agent.yaml can be applied directly:
  arctl apply -f NAME/agent.yaml

Supported frameworks: adk
Supported languages:  python (for adk)`,
		Example: `  arctl init agent adk python myagent
  arctl init agent adk python myagent --model-provider openai --model-name gpt-4o
  arctl init agent adk python myagent --git https://github.com/acme/myagent
  arctl init agent adk python myagent --mcp acme/fetch@1.0.0 --skill summarize --prompt system-prompt`,
		Args:         cobra.ExactArgs(3),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			framework := strings.ToLower(args[0])
			language := strings.ToLower(args[1])
			name := args[2]

			if err := validateInitFrameworkAndLanguage(framework, language); err != nil {
				return err
			}
			if err := validators.ValidateAgentName(name); err != nil {
				return fmt.Errorf("invalid agent name: %w", err)
			}

			modelProvider, err := normalizeInitModelProvider(initModelProvider)
			if err != nil {
				return err
			}
			modelName := resolveInitModelName(cmd, modelProvider, initModelName)

			image := initImage
			if image == "" {
				registry := strings.TrimSuffix(version.DockerRegistry, "/")
				if registry == "" {
					registry = "localhost:5001"
				}
				image = fmt.Sprintf("%s/%s:latest", registry, name)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			projectDir := filepath.Join(cwd, name)

			// Scaffold all code files (Dockerfile, Python source, etc.) using
			// the existing framework generator. This also writes a flat agent.yaml
			// which we overwrite below with the declarative format.
			generator, err := agentframeworks.NewGenerator(framework, language)
			if err != nil {
				return err
			}
			agentConfig := &agentcommon.AgentConfig{
				Name:                  name,
				Version:               initVersion,
				Description:           initDescription,
				Image:                 image,
				Directory:             projectDir,
				ModelProvider:         modelProvider,
				ModelName:             modelName,
				Framework:             framework,
				Language:              language,
				KagentADKImageVersion: "0.8.0-beta6",
				KagentADKPyVersion:    "0.8.0b6",
				Port:                  8080,
				InitGit:               true,
			}
			if err := generator.Generate(agentConfig); err != nil {
				return err
			}

			// Overwrite agent.yaml with the declarative format.
			if err := writeDeclarativeAgentYAML(projectDir, name, initVersion, image, language, framework, modelProvider, modelName, initDescription, initGit, initMCPs, initSkills, initPrompts); err != nil {
				return fmt.Errorf("writing declarative agent.yaml: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✓ Successfully created agent: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. cd %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. (Optional) Build and push the image if developing locally:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl build . --push\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  3. Publish the agent to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f agent.yaml\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "Agent description")
	cmd.Flags().StringVar(&initModelProvider, "model-provider", "Gemini", "Model provider (OpenAI, Anthropic, Gemini, AzureOpenAI, Agentgateway)")
	cmd.Flags().StringVar(&initModelName, "model-name", "gemini-2.0-flash", "Model name")
	cmd.Flags().StringVar(&initImage, "image", "", "Docker image (default: localhost:5001/<name>:latest)")
	cmd.Flags().StringVar(&initGit, "git", "", "Git repository URL (GitHub, GitLab, Bitbucket)")
	cmd.Flags().StringArrayVar(&initMCPs, "mcp", nil, "Registry MCP server to reference: name[@version] (repeatable)")
	cmd.Flags().StringArrayVar(&initSkills, "skill", nil, "Registry skill to reference: name[@version] (repeatable)")
	cmd.Flags().StringArrayVar(&initPrompts, "prompt", nil, "Registry prompt to reference: name[@version] (repeatable)")

	return cmd
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

// localMCPName returns the local name for an MCP server reference.
// For namespace/name format, returns the name part; otherwise returns as-is.
func localMCPName(registryName string) string {
	if i := strings.LastIndex(registryName, "/"); i >= 0 {
		return registryName[i+1:]
	}
	return registryName
}

// writeDeclarativeAgentYAML writes agent.yaml in the ar.dev/v1alpha1 declarative format.
// Uses the typed kinds.AgentSpec struct instead of map[string]any to ensure compile-time
// field validation and consistent YAML key naming.
func writeDeclarativeAgentYAML(projectDir, name, ver, image, language, framework, modelProvider, modelName, description, gitURL string, mcps, skills, prompts []string) error {
	registryURL := agentutils.GetDefaultRegistryURL()

	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s agent", name)
	}

	spec := kinds.AgentSpec{
		Image:         image,
		Language:      language,
		Framework:     framework,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		Description:   desc,
	}

	if gitURL != "" {
		spec.Repository = &kinds.AgentRepository{
			URL:    gitURL,
			Source: "git",
		}
	}

	for _, raw := range mcps {
		serverName, mcpVer := parseNameVersion(raw)
		spec.McpServers = append(spec.McpServers, kinds.AgentMcpServer{
			Type:                  "registry",
			Name:                  localMCPName(serverName),
			RegistryURL:           registryURL,
			RegistryServerName:    serverName,
			RegistryServerVersion: mcpVer,
		})
	}

	for _, raw := range skills {
		skillName, skillVer := parseNameVersion(raw)
		spec.Skills = append(spec.Skills, kinds.AgentSkillRef{
			Name:                 skillName,
			RegistryURL:          registryURL,
			RegistrySkillName:    skillName,
			RegistrySkillVersion: skillVer,
		})
	}

	for _, raw := range prompts {
		promptName, promptVer := parseNameVersion(raw)
		spec.Prompts = append(spec.Prompts, kinds.AgentPromptRef{
			Name:                  promptName,
			RegistryURL:           registryURL,
			RegistryPromptName:    promptName,
			RegistryPromptVersion: promptVer,
		})
	}

	doc := struct {
		APIVersion string          `yaml:"apiVersion"`
		Kind       string          `yaml:"kind"`
		Metadata   kinds.Metadata  `yaml:"metadata"`
		Spec       kinds.AgentSpec `yaml:"spec"`
	}{
		APIVersion: scheme.APIVersion,
		Kind:       "Agent",
		Metadata:   kinds.Metadata{Name: name, Version: ver},
		Spec:       spec,
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "agent.yaml"), b, 0o644)
}

var supportedInitModelProviders = map[string]struct{}{
	"openai":       {},
	"anthropic":    {},
	"gemini":       {},
	"azureopenai":  {},
	"agentgateway": {},
}

func validateInitFrameworkAndLanguage(framework, language string) error {
	if framework != "adk" {
		return fmt.Errorf("unsupported framework %q — only 'adk' is supported", framework)
	}
	if language != "python" {
		return fmt.Errorf("unsupported language %q for framework 'adk' — only 'python' is supported", language)
	}
	return nil
}

func normalizeInitModelProvider(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "", nil
	}
	if _, ok := supportedInitModelProviders[trimmed]; !ok {
		return "", fmt.Errorf("unsupported model provider %q — supported: OpenAI, Anthropic, Gemini, AzureOpenAI, Agentgateway", value)
	}
	return trimmed, nil
}

func resolveInitModelName(cmd *cobra.Command, modelProvider, modelName string) string {
	providerChanged := cmd.Flags().Changed("model-provider")
	modelNameChanged := cmd.Flags().Changed("model-name")
	name := strings.TrimSpace(modelName)
	if providerChanged && !modelNameChanged {
		if defaultName, ok := defaultInitModelName(modelProvider); ok {
			return defaultName
		}
	}
	return name
}

func defaultInitModelName(provider string) (string, bool) {
	switch provider {
	case "openai", "agentgateway":
		return "gpt-4o-mini", true
	case "anthropic":
		return "claude-3-5-sonnet", true
	case "gemini":
		return "gemini-2.0-flash", true
	case "azureopenai":
		return "your-deployment-name", true
	default:
		return "", false
	}
}

// --- mcp init ---

var supportedMCPFrameworks = map[string]struct{}{
	"fastmcp-python": {},
	"mcp-go":         {},
}

func newInitMCPCmd() *cobra.Command {
	var (
		initVersion     string
		initDescription string
		initImage       string
	)

	cmd := &cobra.Command{
		Use:   "mcp FRAMEWORK NAMESPACE/NAME",
		Short: "Scaffold a new MCP server project with declarative mcp.yaml",
		Long: `Scaffold a new MCP server project. Creates a project directory
containing a declarative mcp.yaml (ar.dev/v1alpha1) and source stubs.

NAME must be in namespace/name format as required by the registry.

The generated mcp.yaml can be applied directly:
  arctl apply -f NAME/mcp.yaml

Supported frameworks: fastmcp-python, mcp-go`,
		Example: `  arctl init mcp fastmcp-python myorg/my-server
  arctl init mcp mcp-go myorg/my-server --version 1.0.0`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			framework := strings.ToLower(args[0])
			fullName := args[1]

			if _, ok := supportedMCPFrameworks[framework]; !ok {
				return fmt.Errorf("unsupported framework %q — supported: fastmcp-python, mcp-go", framework)
			}
			if err := validators.ValidateMCPServerName(fullName); err != nil {
				return fmt.Errorf("invalid MCP server name: %w", err)
			}

			// Use just the name part (after /) as the project directory name.
			parts := strings.SplitN(fullName, "/", 2)
			dirName := parts[len(parts)-1]

			image := initImage
			if image == "" {
				registry := strings.TrimSuffix(version.DockerRegistry, "/")
				if registry == "" {
					registry = "localhost:5001"
				}
				image = fmt.Sprintf("%s/%s:latest", registry, dirName)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			projectDir := filepath.Join(cwd, dirName)

			generator, err := mcpframeworks.GetGenerator(framework)
			if err != nil {
				return err
			}
			cfg := mcptemplates.ProjectConfig{
				ProjectName: dirName,
				Version:     initVersion,
				Description: initDescription,
				Directory:   projectDir,
				NoGit:       false,
			}
			if err := generator.GenerateProject(cfg); err != nil {
				return fmt.Errorf("generating MCP project: %w", err)
			}

			if err := writeDeclarativeMCPYAML(projectDir, fullName, initVersion, image, initDescription); err != nil {
				return fmt.Errorf("writing declarative mcp.yaml: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✓ Successfully created MCP server: %s\n", fullName)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. cd %s\n", dirName)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. (Optional) Build and push the image if developing locally:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl build . --push\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  3. Publish the MCP server to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f mcp.yaml\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "MCP server description")
	cmd.Flags().StringVar(&initImage, "image", "", "Docker image (default: localhost:5001/<name>:latest)")

	return cmd
}

func writeDeclarativeMCPYAML(projectDir, name, ver, image, description string) error {
	nameParts := strings.SplitN(name, "/", 2)
	shortName := nameParts[len(nameParts)-1]

	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s MCP server", shortName)
	}

	spec := kinds.MCPSpec{
		Title:       shortName,
		Description: desc,
		Packages: []kinds.MCPPackage{
			{
				RegistryType: "oci",
				Identifier:   image,
				Version:      ver,
				Transport:    kinds.MCPTransport{Type: "stdio"},
			},
		},
	}

	doc := struct {
		APIVersion string         `yaml:"apiVersion"`
		Kind       string         `yaml:"kind"`
		Metadata   kinds.Metadata `yaml:"metadata"`
		Spec       kinds.MCPSpec  `yaml:"spec"`
	}{
		APIVersion: scheme.APIVersion,
		Kind:       "MCPServer",
		Metadata:   kinds.Metadata{Name: name, Version: ver},
		Spec:       spec,
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "mcp.yaml"), b, 0o644)
}

// --- skill init ---

func newInitSkillCmd() *cobra.Command {
	var (
		initVersion     string
		initDescription string
		initCategory    string
	)

	cmd := &cobra.Command{
		Use:   "skill NAME",
		Short: "Scaffold a new skill project with declarative skill.yaml",
		Long: `Scaffold a new skill project. Creates a project directory
containing a declarative skill.yaml (ar.dev/v1alpha1) and source stubs.

The generated skill.yaml can be applied directly:
  arctl apply -f NAME/skill.yaml`,
		Example: `  arctl init skill my-skill
  arctl init skill my-skill --category nlp --description "Text summarizer"`,
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			if err := validators.ValidateSkillName(name); err != nil {
				return fmt.Errorf("invalid skill name: %w", err)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			projectDir := filepath.Join(cwd, name)

			if err := skilltemplates.NewGenerator().GenerateProject(skilltemplates.ProjectConfig{
				ProjectName: name,
				Directory:   projectDir,
				NoGit:       false,
			}); err != nil {
				return fmt.Errorf("generating skill project: %w", err)
			}

			if err := writeDeclarativeSkillYAML(projectDir, name, initVersion, initDescription, initCategory); err != nil {
				return fmt.Errorf("writing declarative skill.yaml: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✓ Successfully created skill: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. cd %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. (Optional) Build and push the image if developing locally:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl build . --push\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  3. Publish the skill to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f skill.yaml\n")
			return nil
		},
	}

	cmd.Flags().StringVar(&initVersion, "version", "0.1.0", "Initial version")
	cmd.Flags().StringVar(&initDescription, "description", "", "Skill description")
	cmd.Flags().StringVar(&initCategory, "category", "general", "Skill category (e.g. nlp, general)")

	return cmd
}

func writeDeclarativeSkillYAML(projectDir, name, ver, description, category string) error {
	desc := description
	if desc == "" {
		desc = fmt.Sprintf("%s skill", name)
	}

	spec := kinds.SkillSpec{
		Title:       name,
		Category:    category,
		Description: desc,
	}

	doc := struct {
		APIVersion string          `yaml:"apiVersion"`
		Kind       string          `yaml:"kind"`
		Metadata   kinds.Metadata  `yaml:"metadata"`
		Spec       kinds.SkillSpec `yaml:"spec"`
	}{
		APIVersion: scheme.APIVersion,
		Kind:       "Skill",
		Metadata:   kinds.Metadata{Name: name, Version: ver},
		Spec:       spec,
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	return os.WriteFile(filepath.Join(projectDir, "skill.yaml"), b, 0o644)
}

// --- prompt init ---

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
		Args:         cobra.ExactArgs(1),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			// Prompt names follow the same DB constraint as skill names (^[a-zA-Z0-9_-]+$).
			if err := validators.ValidateSkillName(name); err != nil {
				return fmt.Errorf("invalid prompt name: %w", err)
			}

			cwd, err := os.Getwd()
			if err != nil {
				return fmt.Errorf("getting working directory: %w", err)
			}
			// Prompts are just a YAML file — no project directory needed.
			outPath := filepath.Join(cwd, name+".yaml")

			if err := writeDeclarativePromptYAML(outPath, name, initVersion, initDescription, initContent); err != nil {
				return fmt.Errorf("writing declarative prompt.yaml: %w", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "✓ Successfully created prompt: %s\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "\n🚀 Next steps:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "  1. Edit %s.yaml if needed\n", name)
			fmt.Fprintf(cmd.OutOrStdout(), "  2. Publish the prompt to the registry:\n")
			fmt.Fprintf(cmd.OutOrStdout(), "     arctl apply -f %s.yaml\n", name)
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

	spec := kinds.PromptSpec{
		Description: desc,
		Content:     content,
	}

	doc := struct {
		APIVersion string           `yaml:"apiVersion"`
		Kind       string           `yaml:"kind"`
		Metadata   kinds.Metadata   `yaml:"metadata"`
		Spec       kinds.PromptSpec `yaml:"spec"`
	}{
		APIVersion: scheme.APIVersion,
		Kind:       "Prompt",
		Metadata:   kinds.Metadata{Name: name, Version: ver},
		Spec:       spec,
	}

	b, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}

	return os.WriteFile(path, b, 0o644)
}

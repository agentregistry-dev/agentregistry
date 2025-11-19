package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/spf13/cobra"
)

const adkBaseImageVersion = "0.7.4"

var InitCmd = &cobra.Command{
	Use:   "init [framework] [language] [agent-name]",
	Short: "Initialize a new agent project",
	Long: `Initialize a new agent project using the specified framework and language.

You can customize the root agent instructions using the --instruction-file flag.
You can select a specific model using --model-provider and --model-name flags.
If no custom instruction file is provided, a default dice-rolling instruction will be used.
If no model is specified, the agent will need to be configured later.

Examples:
arctl agent init adk python dice
arctl agent init adk python dice --instruction-file instructions.md
arctl agent init adk python dice --model-provider Gemini --model-name gemini-2.0-flash`,
	Args:    cobra.ExactArgs(3),
	RunE:    runInit,
	Example: `arctl agent init adk python dice`,
}

var (
	initInstructionFile string
	initModelProvider   string
	initModelName       string
	initDescription     string
)

func init() {
	InitCmd.Flags().StringVar(&initInstructionFile, "instruction-file", "", "Path to file containing custom instructions for the root agent")
	InitCmd.Flags().StringVar(&initModelProvider, "model-provider", "Gemini", "Model provider (OpenAI, Anthropic, Gemini, AzureOpenAI)")
	InitCmd.Flags().StringVar(&initModelName, "model-name", "gemini-2.0-flash", "Model name (e.g., gpt-4, claude-3-5-sonnet, gemini-2.0-flash)")
	InitCmd.Flags().StringVar(&initDescription, "description", "", "Description for the agent")
}

func runInit(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	framework := strings.ToLower(args[0])
	language := strings.ToLower(args[1])
	agentName := args[2]

	if err := validateFrameworkAndLanguage(framework, language); err != nil {
		return err
	}

	modelProvider, err := normalizeModelProvider(initModelProvider)
	if err != nil {
		return err
	}

	modelName := strings.TrimSpace(initModelName)
	if modelName != "" && modelProvider == "" {
		return fmt.Errorf("model provider is required when model name is provided")
	}

	instruction, err := loadInstruction(initInstructionFile)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current working directory: %w", err)
	}

	projectDir := filepath.Join(cwd, agentName)
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		return fmt.Errorf("failed to create project directory: %w", err)
	}

	generator, err := frameworks.NewGenerator(framework, language)
	if err != nil {
		return err
	}

	agentConfig := &common.AgentConfig{
		Name:          agentName,
		Description:   initDescription,
		Image:         defaultImage(agentName),
		Directory:     projectDir,
		Verbose:       verbose,
		Instruction:   instruction,
		ModelProvider: modelProvider,
		ModelName:     modelName,
		Framework:     framework,
		Language:      language,
		CLIVersion:    adkBaseImageVersion,
		InitGit:       true,
	}

	if err := generator.Generate(agentConfig); err != nil {
		return err
	}

	fmt.Printf("✓ Successfully created agent: %s\n", agentName)
	printAgentNextSteps(agentName)
	return nil
}

func validateFrameworkAndLanguage(framework, language string) error {
	if framework != "adk" {
		return fmt.Errorf("unsupported framework: %s. Only 'adk' is supported", framework)
	}
	if language != "python" {
		return fmt.Errorf("unsupported language: %s. Only 'python' is supported for ADK", language)
	}
	return nil
}

var supportedModelProviders = map[string]struct{}{
	"openai":      {},
	"anthropic":   {},
	"gemini":      {},
	"azureopenai": {},
}

func normalizeModelProvider(value string) (string, error) {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		return "", nil
	}
	if _, ok := supportedModelProviders[trimmed]; !ok {
		return "", fmt.Errorf("unsupported model provider: %s. Supported providers: OpenAI, Anthropic, Gemini, AzureOpenAI", value)
	}
	return trimmed, nil
}

func loadInstruction(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}

	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("failed to read instruction file %q: %w", path, err)
	}
	return string(content), nil
}

func defaultImage(agentName string) string {
	registry := strings.TrimSuffix(version.DockerRegistry, "/")
	if registry == "" {
		registry = "localhost:5001"
	}
	return fmt.Sprintf("%s/%s:latest", registry, agentName)
}

func printAgentNextSteps(agentName string) {
	fmt.Printf("   Note: MCP server directories are created when you run 'arctl agent add-mcp'\n")
	fmt.Printf("\n🚀 Next steps:\n")
	fmt.Printf("   1. cd %s\n", agentName)
	fmt.Printf("   2. Customize your agent in %s/agent.py\n", agentName)
	fmt.Printf("   3. Build the agent image (add --push to publish to your registry)\n")
	fmt.Printf("      arctl agent build .\n")
	fmt.Printf("   4. Run the agent locally\n")
	fmt.Printf("      arctl agent run .\n")
	fmt.Printf("   5. Publish the agent to AgentRegistry\n")
	fmt.Printf("      arctl agent publish . --image %s/%s:latest\n", strings.TrimSuffix(version.DockerRegistry, "/"), agentName)
}

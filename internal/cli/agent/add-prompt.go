package agent

import (
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var AddPromptCmd = &cobra.Command{
	Use:   "add-prompt <name>",
	Short: "Add a prompt to the agent",
	Long: `Add a prompt to the agent manifest.

Prompts can be added from the registry using --registry-prompt-name.
If --registry-url is not provided, the current registry URL is used.

Examples:
  arctl agent add-prompt my-prompt --registry-prompt-name cool-prompt
  arctl agent add-prompt my-prompt --registry-prompt-name cool-prompt --registry-prompt-version 1.0.0
  arctl agent add-prompt my-prompt --registry-prompt-name cool-prompt --registry-url https://registry.example.com
`,
	Args: cobra.ExactArgs(1),
	RunE: runAddPrompt,
}

var (
	promptProjectDir            string
	promptRegistryURL           string
	promptRegistryPromptName    string
	promptRegistryPromptVersion string
)

func init() {
	AddPromptCmd.Flags().StringVar(&promptProjectDir, "project-dir", ".", "Project directory (default: current directory)")
	AddPromptCmd.Flags().StringVar(&promptRegistryURL, "registry-url", "", "Registry URL (default: current registry)")
	AddPromptCmd.Flags().StringVar(&promptRegistryPromptName, "registry-prompt-name", "", "Prompt name in the registry")
	AddPromptCmd.Flags().StringVar(&promptRegistryPromptVersion, "registry-prompt-version", "", "Version of the prompt to pull from the registry")

	_ = AddPromptCmd.MarkFlagRequired("registry-prompt-name")
}

func runAddPrompt(cmd *cobra.Command, args []string) error {
	name := args[0]

	if err := addPromptCmd(name); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	return nil
}

func addPromptCmd(name string) error {
	resolvedDir, err := project.ResolveProjectDir(promptProjectDir)
	if err != nil {
		return err
	}
	manifest, err := project.LoadManifest(resolvedDir)
	if err != nil {
		return err
	}
	if verbose {
		fmt.Printf("Loaded manifest for agent '%s' from %s\n", manifest.Name, resolvedDir)
	}

	ref := buildPromptRef(name)
	if err := checkDuplicatePrompt(manifest, ref.Name); err != nil {
		return err
	}
	manifest.Prompts = append(manifest.Prompts, ref)
	slices.SortFunc(manifest.Prompts, func(a, b models.PromptRef) int {
		return strings.Compare(a.Name, b.Name)
	})

	manager := common.NewManifestManager(resolvedDir)
	if err := manager.Save(manifest); err != nil {
		return fmt.Errorf("failed to save agent.yaml: %w", err)
	}
	printer.PrintSuccess(fmt.Sprintf("Added prompt '%s' to agent.yaml", ref.Name))
	return nil
}

func buildPromptRef(name string) models.PromptRef {
	// Default to the current registry URL if not explicitly provided
	url := promptRegistryURL
	if url == "" {
		url = agentutils.GetDefaultRegistryURL()
	}

	return models.PromptRef{
		Name:                  name,
		RegistryURL:           url,
		RegistryPromptName:    promptRegistryPromptName,
		RegistryPromptVersion: promptRegistryPromptVersion,
	}
}

func checkDuplicatePrompt(manifest *models.AgentManifest, name string) error {
	for _, existing := range manifest.Prompts {
		if strings.EqualFold(existing.Name, name) {
			return fmt.Errorf("a prompt named '%s' already exists in agent.yaml", name)
		}
	}
	return nil
}

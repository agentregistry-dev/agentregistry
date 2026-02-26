package prompt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
	yaml "gopkg.in/yaml.v3"
)

var (
	publishName        string
	publishVersion     string
	publishDescription string
	dryRunFlag         bool
)

var PublishCmd = &cobra.Command{
	Use:   "publish <file>",
	Short: "Publish a prompt to the registry",
	Long: `Publish a prompt to the agent registry.

The file can be:
  - A plain text file (.txt, .md, etc.) containing the prompt content.
    Use --name and --version flags to set metadata.
  - A YAML file (.yaml, .yml) with structured prompt definition
    (name, version, description, content fields).

Examples:
  arctl prompt publish system-prompt.txt --name my-prompt --version 1.0.0
  arctl prompt publish system-prompt.txt --name my-prompt --version 1.0.0 --description "System prompt for code review"
  arctl prompt publish prompt.yaml`,
	Args: cobra.ExactArgs(1),
	RunE: runPublish,
}

func init() {
	PublishCmd.Flags().StringVar(&publishName, "name", "", "Prompt name (required for text files)")
	PublishCmd.Flags().StringVar(&publishVersion, "version", "", "Prompt version (required for text files)")
	PublishCmd.Flags().StringVar(&publishDescription, "description", "", "Prompt description")
	PublishCmd.Flags().BoolVar(&dryRunFlag, "dry-run", false, "Show what would be done without actually doing it")
}

func runPublish(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return fmt.Errorf("failed to resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if os.IsNotExist(err) {
		return fmt.Errorf("file does not exist: %s", absPath)
	}
	if err != nil {
		return fmt.Errorf("failed to stat file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("%s is a directory; pass a file path instead (e.g., prompt.txt or prompt.yaml)", absPath)
	}

	var promptJSON *models.PromptJSON

	ext := strings.ToLower(filepath.Ext(absPath))
	if ext == ".yaml" || ext == ".yml" {
		promptJSON, err = readPromptYAML(absPath)
		if err != nil {
			return fmt.Errorf("failed to read YAML prompt: %w", err)
		}
		// CLI flags override YAML values when set
		if publishName != "" {
			promptJSON.Name = publishName
		}
		if publishVersion != "" {
			promptJSON.Version = publishVersion
		}
		if publishDescription != "" {
			promptJSON.Description = publishDescription
		}
	} else {
		promptJSON, err = readTextPrompt(absPath)
		if err != nil {
			return fmt.Errorf("failed to read prompt file: %w", err)
		}
	}

	if promptJSON.Name == "" {
		return fmt.Errorf("prompt name is required (use --name flag)")
	}
	if promptJSON.Version == "" {
		return fmt.Errorf("prompt version is required (use --version flag)")
	}

	printer.PrintInfo(fmt.Sprintf("Publishing prompt '%s' version %s from: %s", promptJSON.Name, promptJSON.Version, absPath))

	if dryRunFlag {
		j, _ := json.MarshalIndent(promptJSON, "", "  ")
		printer.PrintInfo("[DRY RUN] Would publish:\n" + string(j))
	} else {
		_, err = apiClient.CreatePrompt(promptJSON)
		if err != nil {
			return fmt.Errorf("failed to publish prompt: %w", err)
		}
		printer.PrintSuccess(fmt.Sprintf("Prompt '%s' version %s published successfully!", promptJSON.Name, promptJSON.Version))
	}

	return nil
}

func readTextPrompt(filePath string) (*models.PromptJSON, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	return &models.PromptJSON{
		Name:        publishName,
		Version:     publishVersion,
		Description: publishDescription,
		Content:     string(data),
	}, nil
}

func readPromptYAML(filePath string) (*models.PromptJSON, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read YAML file: %w", err)
	}

	var promptJSON models.PromptJSON
	if err := yaml.Unmarshal(data, &promptJSON); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &promptJSON, nil
}

package prompt

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var deleteVersion string

var DeleteCmd = &cobra.Command{
	Use:   "delete <prompt-name>",
	Short: "Delete a prompt from the registry",
	Long: `Delete a prompt from the registry.

Examples:
  arctl prompt delete my-prompt --version 1.0.0`,
	Args: cobra.ExactArgs(1),
	RunE: runDelete,
}

func init() {
	DeleteCmd.Flags().StringVar(&deleteVersion, "version", "", "Specify the version to delete (required)")
	_ = DeleteCmd.MarkFlagRequired("version")
}

func runDelete(cmd *cobra.Command, args []string) error {
	promptName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Delete the prompt
	printer.PrintInfo(fmt.Sprintf("Deleting prompt %s version %s...", promptName, deleteVersion))
	err := apiClient.DeletePrompt(promptName, deleteVersion)
	if err != nil {
		return fmt.Errorf("failed to delete prompt: %w", err)
	}

	printer.PrintSuccess(fmt.Sprintf("Prompt '%s' version %s deleted successfully", promptName, deleteVersion))
	return nil
}

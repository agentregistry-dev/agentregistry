package deployment

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

var DeleteCmd = &cobra.Command{
	Use:   "delete <deployment-id>",
	Short: "Delete a deployment",
	Long: `Delete a deployment by its ID.

Example:
  arctl deployments delete abc12345
  arctl deployments delete abc12345-def6-7890-ghij-klmnopqrstuv`,
	Args:          cobra.ExactArgs(1),
	RunE:          runDelete,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func runDelete(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	deploymentID := args[0]

	// Resolve the full deployment ID (supports prefix matching for truncated IDs)
	fullID, err := resolveDeploymentID(deploymentID)
	if err != nil {
		return err
	}

	dep, err := apiClient.GetDeployment(fullID)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}
	if dep == nil {
		return fmt.Errorf("deployment not found: %s", deploymentID)
	}

	if err := apiClient.DeleteDeployment(fullID); err != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	fmt.Printf("Deployment '%s' (%s %s) deleted\n", dep.ID, dep.ResourceType, dep.ServerName)
	return nil
}

// resolveDeploymentID resolves a potentially truncated deployment ID to its full ID
// by prefix-matching against all deployments.
func resolveDeploymentID(idPrefix string) (string, error) {
	// First try an exact match via the API
	dep, err := apiClient.GetDeployment(idPrefix)
	if err == nil && dep != nil {
		return dep.ID, nil
	}

	// Fall back to prefix matching across all deployments
	deployments, err := apiClient.GetDeployedServers()
	if err != nil {
		return "", fmt.Errorf("failed to list deployments: %w", err)
	}

	var matches []string
	for _, d := range deployments {
		if strings.HasPrefix(d.ID, idPrefix) {
			matches = append(matches, d.ID)
		}
	}

	switch len(matches) {
	case 0:
		return "", fmt.Errorf("deployment not found: %s", idPrefix)
	case 1:
		return matches[0], nil
	default:
		return "", fmt.Errorf("ambiguous deployment ID prefix %q matches %d deployments", idPrefix, len(matches))
	}
}

package deployment

import (
	"context"
	"fmt"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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
	dep, err := cliCommon.FindDeploymentByIDPrefix(context.Background(), apiClient, deploymentID)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}
	if dep == nil {
		return fmt.Errorf("deployment not found: %s", deploymentID)
	}

	if err := apiClient.Delete(cmd.Context(), v1alpha1.KindDeployment, dep.Namespace, dep.Name, dep.Version); err != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	fmt.Printf("Deployment '%s' (%s %s) deleted\n", dep.ID, dep.ResourceType, dep.TargetName)
	return nil
}

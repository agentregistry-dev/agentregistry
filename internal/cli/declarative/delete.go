package declarative

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/spf13/cobra"
)

// DeleteCmd is the cobra command for "delete".
// Tests should use NewDeleteCmd() for a fresh instance.
var DeleteCmd = newDeleteCmd()

// NewDeleteCmd returns a new "delete" cobra command.
func NewDeleteCmd() *cobra.Command {
	return newDeleteCmd()
}

func newDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete (TYPE NAME | -f FILE)",
		Short: "Delete a registry resource version",
		Long: `Delete a registry resource.

File mode (declarative): reads resources from the YAML file and sends DELETE /v0/apply.
  arctl delete -f agent.yaml

Explicit mode: specify type and name. --version is optional and defaults to the latest version.
  arctl delete TYPE NAME [--version VERSION]

For deployments, --version is required.

TYPE must be one of: agent, mcp, skill, prompt, deployment
(plural and uppercase forms also accepted)`,
		Example: `  arctl delete -f my-agent/agent.yaml
  arctl delete -f my-server/mcp.yaml
  arctl delete agent acme/summarizer --version 1.0.0
  arctl delete mcp acme/fetch --version 1.0.0
  arctl delete deployment my-agent --version 1.0.0 --force`,
		SilenceUsage: true,
		RunE:         runDeclarativeDelete,
	}
	cmd.Flags().StringP("filename", "f", "", "YAML file to read resources from")
	cmd.Flags().String("version", "", "Version to delete (defaults to the latest version; required for deployments)")
	cmd.Flags().Bool("force", false, "Skip provider-specific teardown and only remove the registry record (deployments only)")
	return cmd
}

func runDeclarativeDelete(cmd *cobra.Command, args []string) error {
	filename, _ := cmd.Flags().GetString("filename")
	force, _ := cmd.Flags().GetBool("force")

	if filename != "" {
		if force {
			return fmt.Errorf("--force cannot be used with -f; it only applies to explicit deployment deletes")
		}
		return deleteFromFile(cmd, filename)
	}

	// Explicit mode: TYPE NAME [--version VERSION]
	if len(args) != 2 {
		return fmt.Errorf("explicit mode requires TYPE and NAME arguments (or use -f FILE)")
	}
	version, _ := cmd.Flags().GetString("version")
	return deleteResource(cmd, args[0], args[1], version, force)
}

// deleteFromFile reads a YAML file and sends a single DELETE /v0/apply request.
// Per-resource results are printed; non-zero exit if any failed.
func deleteFromFile(cmd *cobra.Command, filename string) error {
	data, err := os.ReadFile(filename)
	if err != nil {
		return err
	}

	// Validate locally so unknown kinds fail before hitting the network.
	if _, err := scheme.DecodeBytes(data); err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	results, err := apiClient.DeleteViaApply(cmd.Context(), data)
	if err != nil {
		return fmt.Errorf("DELETE /v0/apply: %w", err)
	}

	printResults(cmd.OutOrStdout(), results, false)

	for _, r := range results {
		if r.Status == arv0.ApplyStatusFailed {
			return fmt.Errorf("one or more resources failed to delete")
		}
	}
	return nil
}

// deleteResource performs an explicit per-kind delete using the registry to resolve the kind.
func deleteResource(cmd *cobra.Command, typeName, name, version string, force bool) error {
	k, err := scheme.Lookup(typeName)
	if err != nil {
		return err
	}

	if force && k.Kind != "deployment" {
		return fmt.Errorf("--force is only supported for deployments")
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	if version != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s version %s...\n", k.Kind, name, version)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s...\n", k.Kind, name)
	}
	if err := deleteItem(k, name, version, force); err != nil {
		if version != "" {
			return fmt.Errorf("failed to delete %s %q version %s: %w", k.Kind, name, version, err)
		}
		return fmt.Errorf("failed to delete %s %q: %w", k.Kind, name, err)
	}

	if version != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (%s)\n", strings.ToLower(k.Kind), name, version)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s\n", strings.ToLower(k.Kind), name)
	}
	return nil
}

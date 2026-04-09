package declarative

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/resource"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
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
		Long: `Delete a specific version of a registry resource.

File mode (declarative): reads name and version from the YAML file.
  arctl delete -f agent.yaml

Explicit mode: specify type, name, and --version directly.
  arctl delete TYPE NAME --version VERSION

TYPE must be one of: agent, mcp, skill, prompt
(plural and uppercase forms also accepted)`,
		Example: `  arctl delete -f my-agent/agent.yaml
  arctl delete -f my-server/mcp.yaml
  arctl delete agent acme/summarizer --version 1.0.0
  arctl delete mcp acme/fetch --version 1.0.0`,
		SilenceUsage: true,
		RunE:         runDeclarativeDelete,
	}
	cmd.Flags().StringP("filename", "f", "", "YAML file to read name and version from")
	cmd.Flags().String("version", "", "Version to delete (required in explicit mode)")
	return cmd
}

func runDeclarativeDelete(cmd *cobra.Command, args []string) error {
	filename, _ := cmd.Flags().GetString("filename")

	if filename != "" {
		return deleteFromFile(cmd, filename)
	}

	// Explicit mode: TYPE NAME --version VERSION
	if len(args) != 2 {
		return fmt.Errorf("explicit mode requires TYPE and NAME arguments (or use -f FILE)")
	}
	version, _ := cmd.Flags().GetString("version")
	if version == "" {
		return fmt.Errorf("required flag \"version\" not set (or use -f FILE to read from YAML)")
	}
	return deleteResource(cmd, args[0], args[1], version)
}

func deleteFromFile(cmd *cobra.Command, filename string) error {
	resources, err := scheme.DecodeFile(filename)
	if err != nil {
		return fmt.Errorf("reading %s: %w", filename, err)
	}

	errCount := 0
	for _, r := range resources {
		if r.Metadata.Version == "" {
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %s/%s: metadata.version is required for delete\n", r.Kind, r.Metadata.Name)
			errCount++
			continue
		}
		h, err := resource.Lookup(r.Kind)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
			errCount++
			continue
		}
		if err := deleteResource(cmd, h.Singular(), r.Metadata.Name, r.Metadata.Version); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "error: %v\n", err)
			errCount++
		}
	}
	if errCount > 0 {
		return fmt.Errorf("%d error(s) during delete", errCount)
	}
	return nil
}

func deleteResource(cmd *cobra.Command, typeName, name, version string) error {
	h, err := resource.Lookup(typeName)
	if err != nil {
		return err
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s version %s...\n", h.Singular(), name, version)
	if err := h.Delete(apiClient, name, version); err != nil {
		return fmt.Errorf("failed to delete %s %q version %s: %w",
			h.Singular(), name, version, err)
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (%s)\n", strings.ToLower(h.Kind()), name, version)
	return nil
}

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
		Short: "Delete a registry resource",
		Long: `Delete a registry resource.

File mode (declarative): reads resources from the YAML file and sends DELETE /v0/apply.
  arctl delete -f agent.yaml

Explicit mode: specify type and name. For taggable artifacts, --tag selects an
exact tag and defaults to latest.
  arctl delete TYPE NAME [--tag TAG]

TYPE must be one of: agent, mcp, skill, prompt, deployment
(plural and uppercase forms also accepted)`,
		Example: `  arctl delete -f my-agent/agent.yaml
  arctl delete -f my-server/mcp.yaml
  arctl delete agent acme/summarizer --tag stable
  arctl delete agent acme/summarizer --all-tags
  arctl delete mcp acme/fetch --tag stable
  arctl delete deployment my-agent --force`,
		SilenceUsage: true,
		RunE:         runDeclarativeDelete,
	}
	cmd.Flags().StringP("filename", "f", "", "YAML file to read resources from")
	cmd.Flags().String("tag", "", "Specific tag to delete (taggable artifact kinds only; defaults to latest)")
	cmd.Flags().Bool("force", false, "Skip provider-specific teardown and only remove the registry record (deployments only)")
	cmd.Flags().Bool("all-tags", false, "Delete every tag of NAME (taggable artifact kinds only)")
	return cmd
}

func runDeclarativeDelete(cmd *cobra.Command, args []string) error {
	filename, _ := cmd.Flags().GetString("filename")
	force, _ := cmd.Flags().GetBool("force")
	allTags, _ := cmd.Flags().GetBool("all-tags")
	tag, _ := cmd.Flags().GetString("tag")
	allTagsFlag := "--all-tags"
	tagFlag := "--tag"

	if filename != "" {
		if force {
			return fmt.Errorf("--force cannot be used with -f; it only applies to explicit deployment deletes")
		}
		if allTags {
			return fmt.Errorf("%s cannot be used with -f", allTagsFlag)
		}
		return deleteFromFile(cmd, filename)
	}

	// Explicit mode: TYPE NAME [--tag TAG | --all-tags]
	if len(args) != 2 {
		return fmt.Errorf("explicit mode requires TYPE and NAME arguments (or use -f FILE)")
	}
	if allTags {
		if tag != "" {
			return fmt.Errorf("%s and %s are mutually exclusive", tagFlag, allTagsFlag)
		}
		if force {
			return fmt.Errorf("--force cannot be used with %s", allTagsFlag)
		}
		return deleteAllTagsResource(cmd, args[0], args[1])
	}
	return deleteResource(cmd, args[0], args[1], tag, force)
}

// deleteAllTagsResource removes every live tag of (kind, name).
// Errors cleanly when the kind is not a taggable artifact.
func deleteAllTagsResource(cmd *cobra.Command, typeName, name string) error {
	k, err := scheme.Lookup(typeName)
	if err != nil {
		return err
	}
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleting all tags of %s %s...\n", k.Kind, name)
	if err := deleteAllTags(k, name); err != nil {
		return fmt.Errorf("failed to delete all tags of %s %q: %w", k.Kind, name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (all tags)\n", strings.ToLower(k.Kind), name)
	return nil
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
func deleteResource(cmd *cobra.Command, typeName, name, tag string, force bool) error {
	k, err := scheme.Lookup(typeName)
	if err != nil {
		return err
	}

	if force && k.Kind != "deployment" {
		return fmt.Errorf("--force is only supported for deployments")
	}

	// Deployments and runtimes have no tag of their own; rejecting --tag here
	// keeps users from confusing a deployment's target tag (or a runtime's
	// non-existent tag) with the metadata identity used for delete.
	if tag != "" && (k.Kind == "deployment" || k.Kind == "runtime") {
		return fmt.Errorf("--tag is not supported for %s", k.Kind)
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	if tag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s tag %s...\n", k.Kind, name, tag)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s...\n", k.Kind, name)
	}
	if err := deleteItem(k, name, tag, force); err != nil {
		if tag != "" {
			return fmt.Errorf("failed to delete %s %q tag %s: %w", k.Kind, name, tag, err)
		}
		return fmt.Errorf("failed to delete %s %q: %w", k.Kind, name, err)
	}

	if tag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (%s)\n", strings.ToLower(k.Kind), name, tag)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s\n", strings.ToLower(k.Kind), name)
	}
	return nil
}

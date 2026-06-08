package declarative

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

func NewDeleteCmd(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cliruntime.CommandDelete + " (TYPE NAME | -f FILE)",
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
  arctl delete agent acme-summarizer --tag stable
  arctl delete agent acme-summarizer --all-tags
  arctl delete mcp acme-fetch --tag stable
  arctl delete deployment my-agent`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeclarativeDelete(cmd, deps, args)
		},
	}
	cmd.Flags().StringP("filename", "f", "", "YAML file to read resources from")
	cmd.Flags().String("tag", "", "Specific tag to delete (taggable artifact kinds only; defaults to latest)")
	cmd.Flags().Bool("all-tags", false, "Delete every tag of NAME (taggable artifact kinds only)")
	return cmd
}

func runDeclarativeDelete(cmd *cobra.Command, deps cliruntime.Deps, args []string) error {
	kinds := kindRegistry(deps)
	filename, _ := cmd.Flags().GetString("filename")
	allTags, _ := cmd.Flags().GetBool("all-tags")
	tag, _ := cmd.Flags().GetString("tag")
	allTagsFlag := "--all-tags"
	tagFlag := "--tag"

	if deps.Runtime == nil {
		return fmt.Errorf("registry runtime not configured")
	}
	c, err := deps.Runtime.RegistryClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("resolving registry client: %w", err)
	}

	if filename != "" {
		if allTags {
			return fmt.Errorf("%s cannot be used with -f", allTagsFlag)
		}
		return deleteFromFile(cmd, c, filename)
	}

	// Explicit mode: TYPE NAME [--tag TAG | --all-tags]
	if len(args) != 2 {
		return fmt.Errorf("explicit mode requires TYPE and NAME arguments (or use -f FILE)")
	}
	if allTags {
		if tag != "" {
			return fmt.Errorf("%s and %s are mutually exclusive", tagFlag, allTagsFlag)
		}
		return deleteAllTagsResource(cmd, kinds, c, args[0], args[1])
	}

	return deleteResource(cmd, kinds, c, args[0], args[1], tag)
}

// deleteAllTagsResource removes every live tag of (kind, name).
// Errors cleanly when the kind is not a taggable artifact.
func deleteAllTagsResource(cmd *cobra.Command, kinds *scheme.Registry, c *client.Client, typeName, name string) error {
	k, err := kinds.Lookup(typeName)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleting all tags of %s %s...\n", k.Kind, name)
	if err := deleteAllTags(cmd.Context(), c, k, name); err != nil {
		return fmt.Errorf("failed to delete all tags of %s %q: %w", k.Kind, name, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (all tags)\n", strings.ToLower(k.Kind), name)
	return nil
}

// deleteFromFile reads a YAML file and sends a single DELETE /v0/apply request.
// Per-resource results are printed; non-zero exit if any failed.
func deleteFromFile(cmd *cobra.Command, c *client.Client, filename string) error {
	var data []byte
	var err error
	if filename == "-" {
		data, err = io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return fmt.Errorf("reading stdin: %w", err)
		}
	} else {
		data, err = os.ReadFile(filename)
		if err != nil {
			return err
		}
	}

	// Validate locally so unknown kinds fail before hitting the network.
	if _, err := scheme.DecodeBytes(data); err != nil {
		return fmt.Errorf("parsing %s: %w", filename, err)
	}

	results, err := c.DeleteViaApply(cmd.Context(), data)
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
func deleteResource(cmd *cobra.Command, kinds *scheme.Registry, c *client.Client, typeName, name, tag string) error {
	k, err := kinds.Lookup(typeName)
	if err != nil {
		return err
	}

	// Deployments and runtimes have no tag of their own; rejecting --tag here
	// keeps users from confusing a deployment's target tag (or a runtime's
	// non-existent tag) with the metadata identity used for delete.
	if tag != "" && (k.Kind == "deployment" || k.Kind == "runtime") {
		return fmt.Errorf("--tag is not supported for %s", k.Kind)
	}

	if tag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s tag %s...\n", k.Kind, name, tag)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s...\n", k.Kind, name)
	}
	if err := deleteItem(cmd.Context(), c, k, name, tag); err != nil {
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

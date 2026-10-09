package commands

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
	supportedTypes := supportedKindNames(kindRegistry(deps))
	cmd := &cobra.Command{
		Use:   cliruntime.CommandDelete + " (TYPE NAME | -f FILE)",
		Short: "Delete a registry resource by type and name, or from a file",
		Long: `Delete a registry resource by type and name, or from a YAML file.

File mode: read resources from FILE and delete them declaratively.
  arctl delete -f agent.yaml

Explicit mode: specify type and name. For tagged kinds, --tag selects an
exact tag and defaults to latest.
  arctl delete TYPE NAME [--tag TAG | --all-tags]

NAME resolves in the namespace selected by --namespace/-n, else
ARCTL_NAMESPACE, else "default". NAME may also be NAMESPACE/NAME; a namespace
there must match the selected one. In file mode, a selected namespace fills
documents that omit metadata.namespace, and the server rejects documents
whose metadata.namespace differs from it.

TYPE must be one of: ` + supportedTypes + `.
Type names are case-insensitive; singular, plural, and registered aliases are accepted.`,
		Example: `  arctl delete -f my-agent/agent.yaml
  arctl delete -f my-server/mcp.yaml
  arctl delete agent acme-summarizer --tag stable
  arctl delete agent acme-summarizer --all-tags
  arctl delete agent acme-summarizer -n team-a
  arctl delete mcp acme-fetch --tag stable
  arctl delete deployment team-a/my-agent
  arctl delete -f my-agent/agent.yaml -n team-a`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeclarativeDelete(cmd, deps, args)
		},
	}
	cmd.Flags().StringP("filename", "f", "", "YAML file to read resources from")
	cmd.Flags().String("tag", "", "Tagged kinds only: delete a specific tag (defaults to latest)")
	cmd.Flags().Bool("all-tags", false, "Tagged kinds only: delete every tag of NAME")
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
	sel, err := selectedNamespace(deps)
	if err != nil {
		return err
	}
	c, err := deps.Runtime.RegistryClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("resolving registry client: %w", err)
	}

	if filename != "" {
		if allTags {
			return fmt.Errorf("%s cannot be used with -f", allTagsFlag)
		}
		return deleteFromFile(cmd, c, filename, sel)
	}

	// Explicit mode: TYPE NAME [--tag TAG | --all-tags]
	if len(args) != 2 {
		return fmt.Errorf("explicit mode requires TYPE and NAME arguments (or use -f FILE)")
	}
	ref, err := resolveResourceRef(args[1], sel)
	if err != nil {
		return err
	}
	if allTags {
		if tag != "" {
			return fmt.Errorf("%s and %s are mutually exclusive", tagFlag, allTagsFlag)
		}
		return deleteAllTagsResource(cmd, kinds, c, args[0], ref)
	}

	return deleteResource(cmd, kinds, c, args[0], ref, tag)
}

// applyNamespace is the ?namespace= sent with batch apply and delete: the
// selected namespace, or empty to keep each document's own namespace.
func applyNamespace(sel namespaceSelection) string {
	if !sel.Selected {
		return ""
	}
	return sel.Namespace
}

// deleteAllTagsResource removes every live tag of (kind, name).
// Errors cleanly when the kind is not a taggable artifact.
func deleteAllTagsResource(cmd *cobra.Command, kinds *scheme.Registry, c *client.Client, typeName string, ref resourceLookupRef) error {
	k, err := kinds.Lookup(typeName)
	if err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Deleting all tags of %s %s...\n", k.Kind, ref)
	if err := deleteAllTags(cmd.Context(), c, k, ref); err != nil {
		return fmt.Errorf("failed to delete all tags of %s %q: %w", k.Kind, ref, err)
	}
	fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (all tags)\n", strings.ToLower(k.Kind), ref)
	return nil
}

// deleteFromFile reads a YAML file and sends a single DELETE /v0/apply request.
// Per-resource results are printed; non-zero exit if any failed.
func deleteFromFile(cmd *cobra.Command, c *client.Client, filename string, sel namespaceSelection) error {
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

	results, err := c.DeleteViaApply(cmd.Context(), data, client.ApplyOpts{Namespace: applyNamespace(sel)})
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
func deleteResource(cmd *cobra.Command, kinds *scheme.Registry, c *client.Client, typeName string, ref resourceLookupRef, tag string) error {
	k, err := kinds.Lookup(typeName)
	if err != nil {
		return err
	}

	// Mutable namespace/name resources have no tag of their own. ListTags is
	// registered only for tagged artifacts, so use that capability instead of
	// maintaining a second hard-coded list of mutable kinds.
	if tag != "" && k.ListTags == nil {
		return fmt.Errorf("--tag is not supported for %s", k.Kind)
	}

	if tag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s tag %s...\n", k.Kind, ref, tag)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleting %s %s...\n", k.Kind, ref)
	}
	if err := deleteItem(cmd.Context(), c, k, ref, tag); err != nil {
		if tag != "" {
			return fmt.Errorf("failed to delete %s %q tag %s: %w", k.Kind, ref, tag, err)
		}
		return fmt.Errorf("failed to delete %s %q: %w", k.Kind, ref, err)
	}

	if tag != "" {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s (%s)\n", strings.ToLower(k.Kind), ref, tag)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "Deleted: %s/%s\n", strings.ToLower(k.Kind), ref)
	}
	return nil
}

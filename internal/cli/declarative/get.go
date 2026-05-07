package declarative

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// GetCmd is the cobra command for "get".
// Tests should use NewGetCmd() for a fresh instance.
var GetCmd = newGetCmd()

// NewGetCmd returns a new "get" cobra command.
func NewGetCmd() *cobra.Command {
	return newGetCmd()
}

func newGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get TYPE [NAME]",
		Short: "List or retrieve registry resources",
		Long: `List or retrieve registry resources by type.

Supported types: agents, mcps, skills, prompts, providers, deployments
(singular and uppercase forms also accepted, e.g. Agent, agent, agents)

Examples:
  arctl get all
  arctl get agents
  arctl get mcps
  arctl get agent acme/summarizer
  arctl get agent acme/summarizer -o yaml
  arctl get agent acme/summarizer --version 1
  arctl get agent acme/summarizer --all-versions
  arctl get skills -o json`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE:         runGet,
	}
	cmd.Flags().StringP("output", "o", "table", "Output format: table, yaml, json")
	cmd.Flags().String("version", "", "Specific version to fetch (defaults to latest; versioned-artifact kinds only; not allowed with --all-versions)")
	cmd.Flags().Bool("all-versions", false, "List every version of NAME (versioned-artifact kinds only)")
	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")
	allVersions, _ := cmd.Flags().GetBool("all-versions")
	version, _ := cmd.Flags().GetString("version")

	if allVersions && version != "" {
		return fmt.Errorf("--version and --all-versions are mutually exclusive")
	}

	if args[0] == "all" {
		if allVersions {
			return fmt.Errorf("--all-versions cannot be used with `get all`")
		}
		if version != "" {
			return fmt.Errorf("--version cannot be used with `get all`")
		}
		return runGetAll(cmd, outputFormat)
	}

	typeName := args[0]

	k, err := scheme.Lookup(typeName)
	if err != nil {
		return err
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	if allVersions {
		if len(args) != 2 {
			return fmt.Errorf("--all-versions requires NAME")
		}
		name := args[1]
		items, err := listVersions(k, name)
		if err != nil {
			return fmt.Errorf("listing versions of %s %q: %w", k.Kind, name, err)
		}
		if len(items) == 0 {
			fmt.Fprintf(cmd.OutOrStdout(), "No versions of %s %q found.\n", k.Kind, name)
			return nil
		}
		return printItems(cmd, k, items, outputFormat)
	}

	// --version is only meaningful for versioned-artifact (content-registry)
	// kinds. ListVersions is set exclusively on those kinds via typedKind, so
	// it's a stable proxy without coupling get.go to v1alpha1's kind table.
	if version != "" {
		if len(args) != 2 {
			return fmt.Errorf("--version requires NAME")
		}
		if k.ListVersions == nil {
			return fmt.Errorf("--version not supported for kind %q (resource is not versioned)", k.Kind)
		}
	}

	if len(args) == 2 {
		name := args[1]
		item, err := getItem(k, name, version)
		if err != nil {
			return fmt.Errorf("getting %s %q: %w", k.Kind, name, err)
		}
		if item == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %q not found\n", k.Kind, name)
			return nil
		}
		return printItem(cmd, k, item, outputFormat)
	}

	items, err := listItems(k)
	if err != nil {
		return fmt.Errorf("listing %s: %w", kindPlural(k), err)
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No %s found.\n", kindPlural(k))
		return nil
	}
	return printItems(cmd, k, items, outputFormat)
}

func runGetAll(cmd *cobra.Command, outputFormat string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	allKinds := scheme.All()
	first := true
	for _, k := range allKinds {
		items, err := listItems(k)
		if errors.Is(err, errNotListable) {
			continue
		}
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "warning: listing %s: %v\n", kindPlural(k), err)
			continue
		}
		if len(items) == 0 {
			continue
		}
		if !first {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		first = false
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", kindPlural(k))
		if err := printItems(cmd, k, items, outputFormat); err != nil {
			return err
		}
	}
	if first {
		fmt.Fprintln(cmd.OutOrStdout(), "No resources found.")
	}
	return nil
}

// printItem renders a single item.
func printItem(cmd *cobra.Command, k *scheme.Kind, item any, outputFormat string) error {
	switch outputFormat {
	case "yaml":
		r := toYAMLValue(k, item)
		if r == nil {
			return fmt.Errorf("failed to convert %s to YAML", k.Kind)
		}
		return marshalYAML(cmd, r)
	case "json":
		return marshalJSON(cmd, item)
	default:
		t := printer.NewTablePrinter(cmd.OutOrStdout())
		t.SetHeaders(tableColumns(k)...)
		t.AddRow(stringsToAny(tableRow(k, item))...)
		return t.Render()
	}
}

// printItems renders a list of items.
func printItems(cmd *cobra.Command, k *scheme.Kind, items []any, outputFormat string) error {
	switch outputFormat {
	case "yaml":
		for i, item := range items {
			r := toYAMLValue(k, item)
			if r == nil {
				continue
			}
			if i > 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "---")
			}
			if err := marshalYAML(cmd, r); err != nil {
				return err
			}
		}
		return nil
	case "json":
		return marshalJSON(cmd, items)
	default:
		t := printer.NewTablePrinter(cmd.OutOrStdout())
		t.SetHeaders(tableColumns(k)...)
		for _, item := range items {
			t.AddRow(stringsToAny(tableRow(k, item))...)
		}
		return t.Render()
	}
}

func stringsToAny(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

func marshalYAML(cmd *cobra.Command, v any) error {
	b, err := yaml.Marshal(v)
	if err != nil {
		return fmt.Errorf("encoding YAML: %w", err)
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), strings.TrimRight(string(b), "\n")+"\n")
	return err
}

func marshalJSON(cmd *cobra.Command, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	_, err = fmt.Fprintln(cmd.OutOrStdout(), string(b))
	return err
}

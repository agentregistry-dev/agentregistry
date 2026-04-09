package declarative

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
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

Supported types: agents, mcps, skills, prompts
(singular and uppercase forms also accepted, e.g. Agent, agent, agents)

Examples:
  arctl get all
  arctl get agents
  arctl get mcps
  arctl get agent acme/summarizer
  arctl get agent acme/summarizer -o yaml
  arctl get skills -o json`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE:         runGet,
	}
	cmd.Flags().StringP("output", "o", "table", "Output format: table, yaml, json")
	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
	outputFormat, _ := cmd.Flags().GetString("output")

	if args[0] == "all" {
		return runGetAll(cmd, outputFormat)
	}

	typeName := args[0]
	h, err := resource.Lookup(typeName)
	if err != nil {
		return err
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	if len(args) == 2 {
		name := args[1]
		item, err := h.Get(apiClient, name)
		if err != nil {
			return fmt.Errorf("getting %s %q: %w", h.Singular(), name, err)
		}
		if item == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %q not found\n", h.Singular(), name)
			return nil
		}
		return printItem(cmd, h, item, outputFormat)
	}

	items, err := h.List(apiClient)
	if err != nil {
		return fmt.Errorf("listing %s: %w", h.Plural(), err)
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No %s found.\n", h.Plural())
		return nil
	}
	return printItems(cmd, h, items, outputFormat)
}

func runGetAll(cmd *cobra.Command, outputFormat string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	handlers := resource.All()
	first := true
	for _, h := range handlers {
		items, err := h.List(apiClient)
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error listing %s: %v\n", h.Plural(), err)
			continue
		}
		if len(items) == 0 {
			continue
		}
		if !first {
			fmt.Fprintln(cmd.OutOrStdout())
		}
		first = false
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n", h.Plural())
		if err := printItems(cmd, h, items, outputFormat); err != nil {
			return err
		}
	}
	if first {
		fmt.Fprintln(cmd.OutOrStdout(), "No resources found.")
	}
	return nil
}

func printItem(cmd *cobra.Command, h resource.ResourceHandler, item any, outputFormat string) error {
	switch outputFormat {
	case "yaml":
		r := h.ToResource(item)
		if r == nil {
			return fmt.Errorf("failed to convert %s to YAML", h.Singular())
		}
		return marshalYAML(cmd, r)
	case "json":
		return marshalJSON(cmd, item)
	default:
		t := printer.NewTablePrinter(cmd.OutOrStdout())
		t.SetHeaders(h.TableColumns()...)
		t.AddRow(stringsToAny(h.TableRow(item))...)
		return t.Render()
	}
}

func printItems(cmd *cobra.Command, h resource.ResourceHandler, items []any, outputFormat string) error {
	switch outputFormat {
	case "yaml":
		for i, item := range items {
			r := h.ToResource(item)
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
		t.SetHeaders(h.TableColumns()...)
		for _, item := range items {
			t.AddRow(stringsToAny(h.TableRow(item))...)
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

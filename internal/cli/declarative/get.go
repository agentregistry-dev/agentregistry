package declarative

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
)

// NewGetCmd returns a new "get" cobra command.
func NewGetCmd(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get TYPE [NAME]",
		Short: "List or retrieve registry resources",
		Long: `List or retrieve registry resources by type.

Supported types: agents, mcps, skills, prompts, runtimes, deployments
(singular and uppercase forms also accepted, e.g. Agent, agent, agents)

Examples:
  arctl get all
  arctl get agents
  arctl get agents --tag stable          # list rows with a specific tag
  arctl get agents --latest              # list rows pinned to the "latest" tag
  arctl get mcps
  arctl get agent acme-summarizer
  arctl get agent acme-summarizer -o yaml
  arctl get agent acme-summarizer --tag stable
  arctl get agent acme-summarizer --all-tags
  arctl get skills -o json`,
		Args:         cobra.RangeArgs(1, 2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runGet(cmd, deps, args)
		},
	}
	cmd.Flags().StringP("output", "o", "table", "Output format: table, yaml, json")
	cmd.Flags().String("tag", "", "Tagged kinds only. With NAME: fetch one tag (defaults to latest). Without NAME: filter the list to this tag.")
	cmd.Flags().Bool("latest", false, "List mode only: restrict to rows pinned to the literal 'latest' tag (equivalent to --tag latest).")
	cmd.Flags().Bool("all-tags", false, "List every tag of NAME (tagged content kinds only)")
	return cmd
}

func runGet(cmd *cobra.Command, deps cliruntime.Deps, args []string) error {
	kinds := kindRegistry(deps)
	outputFormat, _ := cmd.Flags().GetString("output")
	allTags, _ := cmd.Flags().GetBool("all-tags")
	latest, _ := cmd.Flags().GetBool("latest")
	tag, _ := cmd.Flags().GetString("tag")
	allTagsFlag := "--all-tags"
	tagFlag := "--tag"
	latestFlag := "--latest"

	if allTags && tag != "" {
		return fmt.Errorf("%s and %s are mutually exclusive", tagFlag, allTagsFlag)
	}
	if allTags && latest {
		return fmt.Errorf("%s and %s are mutually exclusive", latestFlag, allTagsFlag)
	}
	if latest && tag != "" {
		return fmt.Errorf("%s and %s are mutually exclusive", tagFlag, latestFlag)
	}

	if args[0] == "all" {
		return runGetAllArg(cmd, deps, kinds, outputFormat, getFlags{
			allTags: allTags,
			latest:  latest,
			tag:     tag,
		})
	}

	typeName := args[0]

	k, err := kinds.Lookup(typeName)
	if err != nil {
		return err
	}

	if allTags {
		return runGetAllTags(cmd, deps, k, args, outputFormat)
	}

	// --tag / --latest are only meaningful for tagged content-registry kinds.
	// ListTags is set exclusively on those kinds via typedKind, so it's a
	// stable proxy without coupling get.go to v1alpha1's kind table.
	if tag != "" && k.ListTags == nil {
		return fmt.Errorf("%s not supported for kind %q (resource is not tagged)", tagFlag, k.Kind)
	}
	if latest && k.ListTags == nil {
		return fmt.Errorf("%s not supported for kind %q (resource is not tagged)", latestFlag, k.Kind)
	}

	if deps.Runtime == nil {
		return fmt.Errorf("registry runtime not configured")
	}
	c, err := deps.Runtime.RegistryClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("resolving registry client: %w", err)
	}

	if len(args) == 2 {
		name := args[1]
		item, err := getItem(cmd.Context(), c, k, name, tag)
		if err != nil {
			return fmt.Errorf("getting %s %q: %w", k.Kind, name, err)
		}
		if item == nil {
			fmt.Fprintf(cmd.OutOrStdout(), "%s %q not found\n", k.Kind, name)
			return nil
		}
		return printItem(cmd, k, item, outputFormat)
	}

	listOpts := scheme.ListOpts{Tag: tag, LatestOnly: latest}
	items, err := listItems(cmd.Context(), c, k, listOpts)
	if err != nil {
		return fmt.Errorf("listing %s: %w", kindPlural(k), err)
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No %s found.\n", kindPlural(k))
		return nil
	}
	return printItems(cmd, k, items, outputFormat)
}

type getFlags struct {
	allTags bool
	latest  bool
	tag     string
}

func runGetAllArg(cmd *cobra.Command, deps cliruntime.Deps, kinds *scheme.Registry, outputFormat string, flags getFlags) error {
	if flags.allTags {
		return fmt.Errorf("--all-tags cannot be used with `get all`")
	}
	if flags.tag != "" {
		return fmt.Errorf("--tag cannot be used with `get all`")
	}
	if flags.latest {
		return fmt.Errorf("--latest cannot be used with `get all`")
	}
	c, err := registryClient(cmd, deps)
	if err != nil {
		return err
	}
	return runGetAll(cmd, kinds, c, outputFormat)
}

func runGetAllTags(cmd *cobra.Command, deps cliruntime.Deps, k *scheme.Kind, args []string, outputFormat string) error {
	if len(args) != 2 {
		return fmt.Errorf("--all-tags requires NAME")
	}
	if k.ListTags == nil {
		return fmt.Errorf("--all-tags not supported for kind %q (resource is not taggable)", k.Kind)
	}
	c, err := registryClient(cmd, deps)
	if err != nil {
		return err
	}
	name := args[1]
	items, err := listTags(cmd.Context(), c, k, name)
	if err != nil {
		return fmt.Errorf("listing tags of %s %q: %w", k.Kind, name, err)
	}
	if len(items) == 0 {
		fmt.Fprintf(cmd.OutOrStdout(), "No tags of %s %q found.\n", k.Kind, name)
		return nil
	}
	return printItems(cmd, k, items, outputFormat)
}

func registryClient(cmd *cobra.Command, deps cliruntime.Deps) (*client.Client, error) {
	if deps.Runtime == nil {
		return nil, fmt.Errorf("registry runtime not configured")
	}
	c, err := deps.Runtime.RegistryClient(cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("resolving registry client: %w", err)
	}
	return c, nil
}

func runGetAll(cmd *cobra.Command, kinds *scheme.Registry, c *client.Client, outputFormat string) error {
	allKinds := kinds.All()
	first := true
	for _, k := range allKinds {
		items, err := listItems(cmd.Context(), c, k, scheme.ListOpts{})
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

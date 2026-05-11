package declarative

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/buildconfig"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"
)

// labelInjectableKinds is the set of envelope kinds that participate in
// arctl.dev/* label auto-injection during `arctl apply`. Skills, prompts, and
// other resources are not buildable artefacts and are therefore unaffected.
var labelInjectableKinds = map[string]struct{}{
	"Agent":     {},
	"MCPServer": {},
}

// ApplyCmd is the cobra command for "apply". It is initialized by newApplyCmd.
// Tests should use NewApplyCmd() to obtain a fresh command instance.
var ApplyCmd = newApplyCmd()

// NewApplyCmd returns a new "apply" cobra command. Each call creates an
// independent command with its own flag state, which is required for testing
// since cobra flags accumulate across Execute() calls on the same command instance.
func NewApplyCmd() *cobra.Command {
	return newApplyCmd()
}

func newApplyCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "apply -f FILE",
		Short: "Apply one or more resources from a YAML file",
		Long: `Apply reads a YAML file (or stdin with -f -) containing one or more resource
documents and applies them via POST /v0/apply.

Each resource is applied atomically; the server reports per-resource status.
Best-effort: per-resource errors are reported without aborting the batch.

Examples:
  arctl apply -f agent.yaml
  arctl apply -f stack.yaml --dry-run
  cat stack.yaml | arctl apply -f -`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runApply(cmd, dryRun)
		},
	}
	cmd.Flags().StringArrayP("filename", "f", nil,
		"YAML file to apply (repeatable; use - for stdin)")
	_ = cmd.MarkFlagRequired("filename")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Validate and simulate without mutating state")
	return cmd
}

func runApply(cmd *cobra.Command, dryRun bool) error {
	filePaths, err := cmd.Flags().GetStringArray("filename")
	if err != nil {
		return fmt.Errorf("getting filename flag: %w", err)
	}

	// 1. Read and validate all input files before sending anything.
	var allData [][]byte
	for _, path := range filePaths {
		var data []byte
		if path == "-" {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return fmt.Errorf("reading stdin: %w", err)
			}
		} else {
			data, err = InjectArctlLabels(path)
			if err != nil {
				return err
			}
		}

		// Validate locally via registry decode — catches unknown kinds before sending.
		if _, err := scheme.DecodeBytes(data); err != nil {
			return fmt.Errorf("parsing %s: %w", path, err)
		}
		allData = append(allData, data)
	}

	// 2. Dry-run with --dry-run uses the server-side dryRun flag.
	// We still need an API client for the batch endpoint (unlike the old per-resource dry-run).
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// 3. Send each file as a separate batch call (preserves document separation).
	var anyFailure bool
	for i, data := range allData {
		results, err := apiClient.Apply(cmd.Context(), data, client.ApplyOpts{
			DryRun: dryRun,
		})
		if err != nil {
			// Request-level error (network, 4xx) — report and continue if multiple files.
			fmt.Fprintf(cmd.ErrOrStderr(), "Error applying %s: %v\n", filePaths[i], err)
			anyFailure = true
			continue
		}
		printResults(cmd.OutOrStdout(), results, dryRun)
		for _, r := range results {
			if r.Status == arv0.ApplyStatusFailed {
				anyFailure = true
			}
		}
	}

	if anyFailure {
		return fmt.Errorf("one or more resources failed to apply")
	}
	return nil
}

// InjectArctlLabels reads the v1alpha1 envelope at yamlPath and, if a sibling
// arctl.yaml exists with framework+language, injects matching arctl.dev/*
// labels into metadata.labels for buildable kinds (Agent, MCPServer). Other
// kinds and files without an arctl.yaml sibling pass through unchanged.
//
// Multi-document YAML files are supported: each document is patched
// independently, then re-emitted as a single multi-doc stream.
func InjectArctlLabels(yamlPath string) ([]byte, error) {
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, err
	}

	cfg, err := buildconfig.Read(filepath.Dir(yamlPath))
	if err != nil {
		// No (or unreadable) arctl.yaml — pass through unchanged.
		return data, nil
	}

	docs, err := splitYAMLDocs(data)
	if err != nil {
		return nil, err
	}

	var injected bool
	for _, doc := range docs {
		if len(doc.Content) == 0 {
			continue
		}
		root := doc.Content[0]
		if root.Kind != yaml.MappingNode {
			continue
		}
		kind := scalarValue(root, "kind")
		if _, ok := labelInjectableKinds[kind]; !ok {
			continue
		}
		meta := findOrCreateMappingChild(root, "metadata")
		labels := findOrCreateMappingChild(meta, "labels")
		upsertLabel(labels, "arctl.dev/framework", cfg.Framework)
		upsertLabel(labels, "arctl.dev/language", cfg.Language)
		injected = true
	}

	if !injected {
		// Nothing to do — return the original bytes (avoid round-trip churn).
		return data, nil
	}

	fmt.Printf("→ Injecting labels from arctl.yaml: arctl.dev/framework=%s, arctl.dev/language=%s\n",
		cfg.Framework, cfg.Language)
	return marshalYAMLDocs(docs)
}

// splitYAMLDocs decodes a multi-document YAML stream into one yaml.Node per
// document.
func splitYAMLDocs(data []byte) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		if err := dec.Decode(&n); err != nil {
			if err == io.EOF {
				break
			}
			return nil, err
		}
		docs = append(docs, &n)
	}
	return docs, nil
}

// marshalYAMLDocs re-serializes a list of YAML documents as a single stream,
// preserving the multi-doc separator.
func marshalYAMLDocs(docs []*yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return nil, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// scalarValue returns the value of a scalar field on a mapping node, or "" if
// the field is missing or non-scalar.
func scalarValue(mapping *yaml.Node, key string) string {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i < len(mapping.Content)-1; i += 2 {
		if mapping.Content[i].Value == key && mapping.Content[i+1].Kind == yaml.ScalarNode {
			return mapping.Content[i+1].Value
		}
	}
	return ""
}

// findOrCreateMappingChild returns the mapping child of parent under key,
// creating an empty mapping if absent.
func findOrCreateMappingChild(parent *yaml.Node, key string) *yaml.Node {
	for i := 0; i < len(parent.Content)-1; i += 2 {
		if parent.Content[i].Value == key {
			return parent.Content[i+1]
		}
	}
	keyN := &yaml.Node{Kind: yaml.ScalarNode, Value: key}
	valN := &yaml.Node{Kind: yaml.MappingNode}
	parent.Content = append(parent.Content, keyN, valN)
	return valN
}

// upsertLabel sets labels[key] = value, creating the entry if missing.
func upsertLabel(labels *yaml.Node, key, value string) {
	for i := 0; i < len(labels.Content)-1; i += 2 {
		if labels.Content[i].Value == key {
			labels.Content[i+1].Value = value
			labels.Content[i+1].Tag = ""
			return
		}
	}
	labels.Content = append(labels.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Value: value})
}

func printResults(out io.Writer, results []arv0.ApplyResult, dryRun bool) {
	for _, r := range results {
		mark := "✓"
		if r.Status == arv0.ApplyStatusFailed {
			mark = "✗"
		}
		fmt.Fprintf(out, "%s %s/%s", mark, r.Kind, r.Name)
		if r.Tag != "" {
			fmt.Fprintf(out, " (%s)", r.Tag)
		}
		fmt.Fprintf(out, " %s", r.Status)
		if dryRun {
			fmt.Fprint(out, " (dry run)")
		}
		if r.Error != "" {
			fmt.Fprintf(out, ": %s", r.Error)
		}
		fmt.Fprintln(out)
	}
}

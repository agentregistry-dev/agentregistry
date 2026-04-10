package declarative

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/resource"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/spf13/cobra"
)

// ApplyCmd is the cobra command for "apply". It is initialized by newApplyCmd.
// Tests should use NewApplyCmd() to obtain a fresh command instance.
var ApplyCmd = newApplyCmd()

// NewApplyCmd returns a new "apply" cobra command. Each call creates an
// independent command with its own flag state, which is required for testing
// since cobra's StringArray flags accumulate across Execute() calls on the
// same command instance.
func NewApplyCmd() *cobra.Command {
	return newApplyCmd()
}

func newApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply -f FILE",
		Short: "Create or update registry resources from YAML files",
		Long: `Apply declarative YAML files to the registry.

Each file may contain one or more resources separated by ---. Supported kinds:
  Agent, MCPServer, Skill, Prompt

Examples:
  arctl apply -f agent.yaml
  arctl apply -f stack.yaml --dry-run
  arctl apply -f agent.yaml -f mcpserver.yaml --overwrite
  cat stack.yaml | arctl apply -f -`,
		SilenceUsage: true,
		RunE:         runApply,
	}
	cmd.Flags().StringArrayP("filename", "f", nil,
		"YAML file to apply (repeatable; use - for stdin)")
	_ = cmd.MarkFlagRequired("filename")
	cmd.Flags().Bool("dry-run", false,
		"Preview what would be applied without making API calls")
	cmd.Flags().Bool("overwrite", false,
		"Overwrite an existing resource version")
	return cmd
}

func runApply(cmd *cobra.Command, _ []string) error {
	filePaths, err := cmd.Flags().GetStringArray("filename")
	if err != nil {
		return fmt.Errorf("getting filename flag: %w", err)
	}
	dryRun, err := cmd.Flags().GetBool("dry-run")
	if err != nil {
		return fmt.Errorf("getting dry-run flag: %w", err)
	}
	overwrite, err := cmd.Flags().GetBool("overwrite")
	if err != nil {
		return fmt.Errorf("getting overwrite flag: %w", err)
	}

	// 1. Parse all input files into resources.
	var allResources []*scheme.Resource
	for _, path := range filePaths {
		var resources []*scheme.Resource
		var readErr error

		if path == "-" {
			data, ioErr := io.ReadAll(os.Stdin)
			if ioErr != nil {
				return fmt.Errorf("reading stdin: %w", ioErr)
			}
			resources, readErr = scheme.DecodeBytes(data)
		} else {
			resources, readErr = scheme.DecodeFile(path)
		}
		if readErr != nil {
			return fmt.Errorf("file %s: %w", path, readErr)
		}
		allResources = append(allResources, resources...)
	}

	// 2. Validate all kinds and required fields before any API call.
	for _, r := range allResources {
		if _, err := resource.Lookup(r.Kind); err != nil {
			return err
		}
		if r.Metadata.Version == "" {
			return fmt.Errorf("%s/%s: metadata.version is required", r.Kind, r.Metadata.Name)
		}
	}

	// 3. Dry-run: just print what would happen.
	if dryRun {
		for _, r := range allResources {
			fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would apply %s/%s\n", strings.ToLower(r.Kind), r.Metadata.Name)
		}
		return nil
	}

	// 4. Apply each resource; continue on error (kubectl-style).
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	var errCount int
	for _, r := range allResources {
		h, _ := resource.Lookup(r.Kind) // already validated above
		if err := h.Apply(apiClient, r, overwrite); err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s/%s: %v\n", strings.ToLower(r.Kind), r.Metadata.Name, err)
			errCount++
			continue
		}
		fmt.Fprintf(cmd.OutOrStdout(), "%s/%s applied\n", strings.ToLower(r.Kind), r.Metadata.Name)
	}

	if errCount > 0 {
		return fmt.Errorf("%d resource(s) failed to apply", errCount)
	}
	return nil
}

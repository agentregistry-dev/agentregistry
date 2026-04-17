package declarative

import (
	"fmt"
	"io"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/spf13/cobra"
)

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
	var force, dryRun bool
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
			return runApply(cmd, force, dryRun)
		},
	}
	cmd.Flags().StringArrayP("filename", "f", nil,
		"YAML file to apply (repeatable; use - for stdin)")
	_ = cmd.MarkFlagRequired("filename")
	cmd.Flags().BoolVar(&force, "force", false,
		"Replace drifted deployments (required when drift is detected)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"Validate and simulate without mutating state")
	return cmd
}

func runApply(cmd *cobra.Command, force, dryRun bool) error {
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
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}

		// Validate locally via registry decode — catches unknown kinds before sending.
		if defaultRegistry != nil {
			if _, err := scheme.DecodeBytes(defaultRegistry, data); err != nil {
				return fmt.Errorf("parsing %s: %w", path, err)
			}
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
			Force:  force,
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
			if r.Status == kinds.StatusFailed {
				anyFailure = true
			}
		}
	}

	if anyFailure {
		return fmt.Errorf("one or more resources failed to apply")
	}
	return nil
}

func printResults(out io.Writer, results []kinds.Result, dryRun bool) {
	for _, r := range results {
		mark := "✓"
		if r.Status == kinds.StatusFailed {
			mark = "✗"
		}
		fmt.Fprintf(out, "%s %s/%s", mark, r.Kind, r.Name)
		if r.Version != "" {
			fmt.Fprintf(out, " (%s)", r.Version)
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

package declarative

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// errAPIClientNotInitialized is returned when the CLI's API client was never
// constructed (typically because PersistentPreRunE did not run).
var errAPIClientNotInitialized = errors.New("API client not initialized")

// WaitCmd is the cobra command for "wait".
// Tests should use NewWaitCmd() for a fresh instance.
var WaitCmd = newWaitCmd()

// NewWaitCmd returns a new "wait" cobra command.
func NewWaitCmd() *cobra.Command { return newWaitCmd() }

func newWaitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wait TYPE NAME",
		Short: "Wait for a registry resource to reach a target state",
		Long: `Wait for a registry resource to reach a target state.

Only deployments are supported. Exit codes:

  0  the deployment reached the requested state
  1  the deployment reached a different terminal state, doesn't exist, or
     the timeout was exceeded

Timeout regimes:

  --timeout=5m   (default) wait up to 5 minutes
  --timeout=0    poll once and return the current state
  --timeout=-1   wait forever

When --tag is omitted and multiple deployments share the target name, the
wait fails with an ambiguity error; pass --tag to pin to a specific
deployment.`,
		Example: `  arctl wait deployment my-agent
  arctl wait deployment my-agent --for=failed
  arctl wait deployment my-agent --for=delete --timeout=10m
  arctl wait deployment my-agent --tag 1.0.0`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE:         runDeclarativeWait,
	}
	cmd.Flags().String("for", "deployed", "Target state to wait for: deployed, failed, undeployed, delete")
	cmd.Flags().Duration("timeout", cliCommon.DefaultWaitTimeout,
		"Maximum time to wait. 0 polls once and exits; negative waits forever.")
	cmd.Flags().Duration("interval", 2*time.Second, "How often to poll the registry")
	cmd.Flags().String("tag", "",
		"Restrict the wait to a specific target tag (defaults to any tag of the named target)")
	return cmd
}

func runDeclarativeWait(cmd *cobra.Command, args []string) error {
	typeName, name := args[0], args[1]
	k, err := scheme.Lookup(typeName)
	if err != nil {
		return err
	}
	if k.Kind != "deployment" {
		return fmt.Errorf("wait is only supported for deployments (got %q)", k.Kind)
	}
	if apiClient == nil {
		return errAPIClientNotInitialized
	}

	forFlag, _ := cmd.Flags().GetString("for")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("interval")
	tag, _ := cmd.Flags().GetString("tag")

	opts := cliCommon.WaitOptions{
		Timeout:      timeout,
		PollInterval: interval,
		Progress: func(status string, elapsed time.Duration) {
			fmt.Fprintf(cmd.ErrOrStderr(), "waiting for deployment/%s (status=%s, %s elapsed)\n",
				name, status, elapsed.Round(time.Second))
		},
	}
	normalizedFor := strings.ToLower(strings.TrimSpace(forFlag))
	switch normalizedFor {
	case "", "deployed":
		opts.TargetStatus = "deployed"
	case "failed", "undeployed":
		opts.TargetStatus = normalizedFor
	case "delete", "deleted":
		opts.TargetDeleted = true
	default:
		return fmt.Errorf("invalid --for value %q (want one of: deployed, failed, undeployed, delete)", forFlag)
	}

	resolve := func(ctx context.Context) (*cliCommon.DeploymentRecord, error) {
		return resolveDeploymentForWait(ctx, name, tag)
	}

	if err := cliCommon.WaitForDeployment(cmd.Context(), resolve, opts); err != nil {
		return err
	}

	if opts.TargetDeleted {
		fmt.Fprintf(cmd.OutOrStdout(), "deployment/%s deleted\n", name)
	} else {
		fmt.Fprintf(cmd.OutOrStdout(), "deployment/%s %s\n", name, opts.TargetStatus)
	}
	return nil
}

func resolveDeploymentForWait(ctx context.Context, name, tag string) (*cliCommon.DeploymentRecord, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	var matches []*cliCommon.DeploymentRecord
	for _, dep := range deployments {
		if dep == nil || dep.TargetName != name {
			continue
		}
		if tag != "" && dep.TargetTag != tag {
			continue
		}
		matches = append(matches, dep)
	}
	switch len(matches) {
	case 0:
		return nil, database.ErrNotFound
	case 1:
		return matches[0], nil
	default:
		tags := make([]string, 0, len(matches))
		for _, m := range matches {
			tags = append(tags, m.TargetTag)
		}
		return nil, fmt.Errorf("multiple deployments share target %q (tags: %s); pass --tag to disambiguate",
			name, strings.Join(tags, ", "))
	}
}

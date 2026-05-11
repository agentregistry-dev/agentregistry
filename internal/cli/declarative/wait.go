package declarative

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

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
  --timeout=-1   wait forever`,
		Example: `  arctl wait deployment my-agent
  arctl wait deployment my-agent --for=failed
  arctl wait deployment my-agent --for=delete --timeout=10m
  arctl wait deployment my-agent --version 1.0.0`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE:         runDeclarativeWait,
	}
	cmd.Flags().String("for", "deployed", "Target state to wait for: deployed, failed, undeployed, delete")
	cmd.Flags().Duration("timeout", cliCommon.DefaultWaitTimeout,
		"Maximum time to wait. 0 polls once and exits; negative waits forever.")
	cmd.Flags().Duration("interval", 2*time.Second, "How often to poll the registry")
	cmd.Flags().String("version", "",
		"Restrict the wait to a specific target version (defaults to any version of the named target)")
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
		return fmt.Errorf("API client not initialized")
	}

	forFlag, _ := cmd.Flags().GetString("for")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	interval, _ := cmd.Flags().GetDuration("interval")
	version, _ := cmd.Flags().GetString("version")

	opts := cliCommon.WaitOptions{
		Timeout:      timeout,
		PollInterval: interval,
		Progress: func(status string, elapsed time.Duration) {
			fmt.Fprintf(cmd.ErrOrStderr(), "waiting for deployment/%s (status=%s, %s elapsed)\n",
				name, status, elapsed.Round(time.Second))
		},
	}
	switch strings.ToLower(strings.TrimSpace(forFlag)) {
	case "delete", "deleted":
		opts.TargetDeleted = true
	case "", "deployed":
		opts.TargetStatus = "deployed"
	default:
		opts.TargetStatus = strings.ToLower(strings.TrimSpace(forFlag))
	}

	resolve := func(ctx context.Context) (*cliCommon.DeploymentRecord, error) {
		return resolveDeploymentForWait(ctx, name, version)
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

func resolveDeploymentForWait(ctx context.Context, name, version string) (*cliCommon.DeploymentRecord, error) {
	deployments, err := cliCommon.ListDeployments(ctx, apiClient)
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep == nil || dep.TargetName != name {
			continue
		}
		if version != "" && dep.TargetVersion != version {
			continue
		}
		return dep, nil
	}
	return nil, database.ErrNotFound
}

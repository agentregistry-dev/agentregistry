package declarative

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

var errRegistryRuntimeNotConfigured = errors.New("registry runtime not configured")

// NewWaitCmd returns a new "wait" cobra command.
func NewWaitCmd(deps cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cliruntime.CommandWait + " TYPE NAME",
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
		Example: `  arctl wait deployment aws-v1
  arctl wait deployment aws-v1 --for=failed
  arctl wait deployment aws-v1 --for=delete --timeout=10m`,
		Args:         cobra.ExactArgs(2),
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDeclarativeWait(cmd, deps, args)
		},
	}
	cmd.Flags().String("for", "deployed", "Target state to wait for: deployed, failed, undeployed, delete")
	cmd.Flags().Duration("timeout", cliCommon.DefaultWaitTimeout,
		"Maximum time to wait. 0 polls once and exits; negative waits forever.")
	return cmd
}

func runDeclarativeWait(cmd *cobra.Command, deps cliruntime.Deps, args []string) error {
	typeName, name := args[0], args[1]
	k, err := kindRegistry(deps).Lookup(typeName)
	if err != nil {
		return err
	}
	if k.Kind != "deployment" {
		return fmt.Errorf("wait is only supported for deployments (got %q)", k.Kind)
	}
	if deps.Runtime == nil {
		return errRegistryRuntimeNotConfigured
	}
	c, err := deps.Runtime.RegistryClient(cmd.Context())
	if err != nil {
		return fmt.Errorf("resolving registry client: %w", err)
	}

	forFlag, _ := cmd.Flags().GetString("for")
	timeout, _ := cmd.Flags().GetDuration("timeout")

	opts := cliCommon.WaitOptions{
		Timeout: timeout,
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
		return resolveDeploymentForWait(ctx, c, name)
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

func resolveDeploymentForWait(ctx context.Context, c *client.Client, name string) (*cliCommon.DeploymentRecord, error) {
	dep, err := client.GetTyped(ctx, c, v1alpha1.KindDeployment,
		v1alpha1.DefaultNamespace, name, "",
		func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
	if err != nil {
		return nil, err
	}
	return cliCommon.DeploymentRecordFromObject(dep), nil
}

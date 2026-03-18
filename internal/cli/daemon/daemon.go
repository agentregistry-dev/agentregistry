package daemon

import (
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/spf13/cobra"
)

// NewDaemonCmd creates the daemon command tree with the given manager.
func NewDaemonCmd(dm types.DaemonManager) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the local registry daemon",
		Long:  "Start, stop, and check the status of the local AgentRegistry daemon (Docker Compose).",
		// Override root PersistentPreRunE — daemon commands don't need an API client.
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return nil
		},
	}

	cmd.AddCommand(newStartCmd(dm))
	cmd.AddCommand(newStopCmd(dm))
	cmd.AddCommand(newStatusCmd(dm))

	return cmd
}

func newStartCmd(dm types.DaemonManager) *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the local registry daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if dm.IsRunning() {
				printer.PrintInfo("Daemon is already running.")
				return nil
			}
			return dm.Start()
		},
	}
}

func newStopCmd(dm types.DaemonManager) *cobra.Command {
	var purge bool

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop the local registry daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dm.IsRunning() {
				printer.PrintInfo("Daemon is not running.")
				return nil
			}
			if purge {
				printer.PrintWarning("This will remove all registry data (published servers, agents, skills, prompts).")
				if err := dm.Purge(); err != nil {
					return err
				}
				printer.PrintSuccess("Daemon stopped and all data removed.")
				return nil
			}
			if err := dm.Stop(); err != nil {
				return err
			}
			printer.PrintSuccess("Daemon stopped.")
			return nil
		},
	}

	cmd.Flags().BoolVar(&purge, "purge", false, "Remove all data volumes (destroys registry data)")
	return cmd
}

func newStatusCmd(dm types.DaemonManager) *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show the status of the local registry daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dm.IsRunning() {
				printer.PrintInfo("Daemon is not running.")
				return nil
			}
			printer.PrintSuccess("Daemon is running.")
			return nil
		},
	}
}

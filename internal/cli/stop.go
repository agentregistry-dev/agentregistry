package cli

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/pkg/daemon"
	"github.com/spf13/cobra"
)

var StopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the daemon",
	Long:  `Stops the agent registry daemon and its associated services.`,
	// Override PersistentPreRunE so we don't auto-start the daemon.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	dm := daemon.NewDaemonManager(nil)

	if !dm.IsRunning() {
		fmt.Println("✓ Daemon is not running")
		return nil
	}

	return dm.Stop()
}

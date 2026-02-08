package cli

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/daemon"
	"github.com/spf13/cobra"
)

var StartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the daemon",
	Long:  `Starts the agent registry daemon and its associated services using Docker Compose.`,
	// Override PersistentPreRunE so we don't auto-start the daemon.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	if !utils.IsDockerComposeAvailable() {
		fmt.Println("Docker compose is not available. Please install docker compose and try again.")
		fmt.Println("See https://docs.docker.com/compose/install/ for installation instructions.")
		return fmt.Errorf("docker compose is not available")
	}

	dm := daemon.NewDaemonManager(nil)

	if dm.IsRunning() {
		fmt.Println("✓ Daemon is already running")
		return nil
	}

	return dm.Start()
}

package cli

import (
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

// StatusCmd shows the status of the daemon and database connectivity.
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of the daemon and database",
	Long:  `Displays the current status of the AgentRegistry daemon, database connectivity, and server version.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func runStatus() error {
	baseURL := client.DefaultBaseURL
	if apiClient != nil {
		baseURL = apiClient.BaseURL
	}

	c := apiClient
	if c == nil {
		c = client.NewClient(baseURL, "")
	}

	printer.PrintInfo(fmt.Sprintf("arctl version %s", version.Version))
	printer.PrintInfo("")

	health, err := c.CheckHealth()
	if err != nil {
		printer.PrintError(fmt.Sprintf("Daemon is not running (%s)", c.BaseURL))
		printer.PrintError("Database is not healthy")
		return nil
	}

	if health.Status == "ok" {
		printer.PrintSuccess(fmt.Sprintf("Daemon is running (%s)", c.BaseURL))
	} else {
		printer.PrintWarning(fmt.Sprintf("Daemon is degraded (%s)", c.BaseURL))
	}

	if health.Database == "ok" {
		printer.PrintSuccess("Database is healthy")
	} else {
		printer.PrintError(fmt.Sprintf("Database is not healthy: %s", health.Database))
	}

	return nil
}

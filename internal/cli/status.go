package cli

import (
	"fmt"
	"net/http"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

// StatusCmd shows the status of the daemon and database connectivity.
var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show status of the daemon and database",
	Long:  `Displays the current status of the AgentRegistry daemon, database connectivity, server version, and registry resource counts.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		return runStatus()
	},
}

func runStatus() error {
	fmt.Printf("arctl version %s\n", version.Version)
	fmt.Println()

	// Determine the base URL for health checks
	baseURL := client.DefaultBaseURL
	if apiClient != nil {
		baseURL = apiClient.BaseURL
	}

	// Check daemon / API health
	daemonStatus := checkDaemonHealth(baseURL)
	if daemonStatus.healthy {
		printer.PrintSuccess(fmt.Sprintf("Daemon is running (%s)", daemonStatus.url))
	} else {
		printer.PrintError(fmt.Sprintf("Daemon is not running (%s)", daemonStatus.url))
		printer.PrintError("Database is not reachable")
		fmt.Println()
		fmt.Println("Registry resources: could not retrieve resources")
		return nil
	}

	// When PersistentPreRunE is skipped (status is in skipCommands),
	// apiClient is nil. Create a lightweight client for querying the daemon.
	if apiClient == nil {
		apiClient = client.NewClient(baseURL, "")
	}

	// Server version
	serverVersion, err := apiClient.GetVersion()
	if err != nil {
		printer.PrintWarning(fmt.Sprintf("Could not retrieve server version: %v", err))
	} else {
		fmt.Printf("  Server version:    %s\n", serverVersion.Version)
		fmt.Printf("  Server commit:     %s\n", serverVersion.GitCommit)
		fmt.Printf("  Server build date: %s\n", serverVersion.BuildTime)
	}

	fmt.Println()

	// Database connectivity (inferred from ability to list resources)
	dbOK := true

	servers, err := apiClient.GetPublishedServers()
	serverCount := 0
	if err != nil {
		printer.PrintWarning(fmt.Sprintf("Could not list MCP servers: %v", err))
		dbOK = false
	} else {
		serverCount = len(servers)
	}

	agents, err := apiClient.GetAgents()
	agentCount := 0
	if err != nil {
		printer.PrintWarning(fmt.Sprintf("Could not list agents: %v", err))
		dbOK = false
	} else {
		agentCount = len(agents)
	}

	skills, err := apiClient.GetSkills()
	skillCount := 0
	if err != nil {
		printer.PrintWarning(fmt.Sprintf("Could not list skills: %v", err))
		dbOK = false
	} else {
		skillCount = len(skills)
	}

	prompts, err := apiClient.GetPrompts()
	promptCount := 0
	if err != nil {
		printer.PrintWarning(fmt.Sprintf("Could not list prompts: %v", err))
		dbOK = false
	} else {
		promptCount = len(prompts)
	}

	if dbOK {
		printer.PrintSuccess("Database is reachable")
	} else {
		printer.PrintWarning("Database may have issues (some queries failed)")
	}

	fmt.Println()
	fmt.Println("Registry resources:")

	tp := printer.NewTablePrinter(nil)
	tp.SetHeaders("Resource", "Count")
	tp.AddRow("MCP Servers", fmt.Sprintf("%d", serverCount))
	tp.AddRow("Agents", fmt.Sprintf("%d", agentCount))
	tp.AddRow("Skills", fmt.Sprintf("%d", skillCount))
	tp.AddRow("Prompts", fmt.Sprintf("%d", promptCount))
	if err := tp.Render(); err != nil {
		return fmt.Errorf("rendering table: %w", err)
	}

	return nil
}

type daemonHealthResult struct {
	healthy bool
	url     string
	err     string
}

func checkDaemonHealth(baseURL string) daemonHealthResult {
	healthURL := baseURL + "/ping"

	httpClient := &http.Client{Timeout: 5 * time.Second}
	resp, err := httpClient.Get(healthURL)
	if err != nil {
		return daemonHealthResult{healthy: false, url: baseURL, err: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return daemonHealthResult{healthy: false, url: baseURL, err: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	return daemonHealthResult{healthy: true, url: baseURL}
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/spf13/cobra"
)

var statusOutputFormat string

var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the status of the daemon and registry",
	Long:  `Displays whether the agent registry daemon is running, the server version, and resource counts.`,
	// Override PersistentPreRunE so we don't auto-start the daemon.
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return nil
	},
	RunE: runStatus,
}

func init() {
	StatusCmd.Flags().StringVarP(&statusOutputFormat, "output", "o", "table", "Output format (table, json)")
}

type statusInfo struct {
	Daemon    string `json:"daemon"`
	API       string `json:"api"`
	Version   string `json:"version,omitempty"`
	GitCommit string `json:"git_commit,omitempty"`
	BuildTime string `json:"build_time,omitempty"`
	Servers   int    `json:"servers"`
	Agents    int    `json:"agents"`
	Skills    int    `json:"skills"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	baseURL := os.Getenv("ARCTL_API_BASE_URL")
	if baseURL == "" {
		baseURL = client.DefaultBaseURL
	}
	token := os.Getenv("ARCTL_API_TOKEN")

	info := statusInfo{
		Daemon:  "unknown",
		API:     "unreachable",
		Servers: -1,
		Agents:  -1,
		Skills:  -1,
	}

	// Try to connect to the API without retries or auto-start.
	c := client.NewClient(baseURL, token)
	if err := c.Ping(); err != nil {
		info.Daemon = "stopped"
		info.API = "unreachable"
	} else {
		info.Daemon = "running"
		info.API = "ok"

		if ver, err := c.GetVersion(); err == nil {
			info.Version = ver.Version
			info.GitCommit = ver.GitCommit
			info.BuildTime = ver.BuildTime
		}

		if servers, err := c.GetPublishedServers(); err == nil {
			info.Servers = len(servers)
		}
		if agents, err := c.GetAgents(); err == nil {
			info.Agents = len(agents)
		}
		if skills, err := c.GetSkills(); err == nil {
			info.Skills = len(skills)
		}
	}

	if statusOutputFormat == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(info)
	}

	// Table output
	fmt.Printf("arctl version:   %s\n", version.Version)
	fmt.Printf("Daemon:          %s\n", info.Daemon)
	fmt.Printf("API:             %s\n", info.API)
	if info.Version != "" {
		fmt.Printf("Server version:  %s\n", info.Version)
		fmt.Printf("Git commit:      %s\n", info.GitCommit)
		fmt.Printf("Build time:      %s\n", info.BuildTime)
	}
	if info.Servers >= 0 {
		fmt.Printf("MCP servers:     %d\n", info.Servers)
	}
	if info.Agents >= 0 {
		fmt.Printf("Agents:          %d\n", info.Agents)
	}
	if info.Skills >= 0 {
		fmt.Printf("Skills:          %d\n", info.Skills)
	}

	return nil
}

package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

type statusResult struct {
	Registry    registryStatus `json:"registry"`
	CLI         cliStatus      `json:"cli"`
	Artifacts   artifactCounts `json:"artifacts"`
	Deployments int            `json:"deployments"`
}

type registryStatus struct {
	URL       string `json:"url"`
	Reachable bool   `json:"reachable"`
	Version   string `json:"version,omitempty"`
}

type cliStatus struct {
	Version   string `json:"version"`
	GitCommit string `json:"git_commit"`
}

type artifactCounts struct {
	MCPServers int `json:"mcp_servers"`
	Agents     int `json:"agents"`
	Skills     int `json:"skills"`
	Prompts    int `json:"prompts"`
}

var statusJSON bool

var StatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show registry connectivity, artifact counts, and CLI info",
	Long: `Displays an at-a-glance health summary: whether the registry is reachable,
how many artifacts are published, active deployments, and CLI/server versions.`,
	Run: func(cmd *cobra.Command, args []string) {
		result := statusResult{
			CLI: cliStatus{
				Version:   version.Version,
				GitCommit: version.GitCommit,
			},
		}

		// Build a best-effort client (status command skips root pre-run)
		c := apiClient
		if c == nil {
			c = client.NewClient(os.Getenv("ARCTL_API_BASE_URL"), os.Getenv("ARCTL_API_TOKEN"))
		}
		result.Registry.URL = c.BaseURL

		// Check connectivity
		if err := c.Ping(); err != nil {
			result.Registry.Reachable = false
			if statusJSON {
				printStatusJSON(result)
			} else {
				printStatusTable(result)
			}
			return
		}
		result.Registry.Reachable = true

		// Server version
		if v, err := c.GetVersion(); err == nil {
			result.Registry.Version = v.Version
		}

		// Artifact counts (best-effort, don't fail on individual errors)
		if servers, err := c.GetPublishedServers(); err == nil {
			result.Artifacts.MCPServers = len(servers)
		}
		if agents, err := c.GetAgents(); err == nil {
			result.Artifacts.Agents = len(agents)
		}
		if skills, err := c.GetSkills(); err == nil {
			result.Artifacts.Skills = len(skills)
		}
		if prompts, err := c.GetPrompts(); err == nil {
			result.Artifacts.Prompts = len(prompts)
		}

		// Deployments
		if deployments, err := c.GetDeployedServers(); err == nil {
			result.Deployments = len(deployments)
		}

		if statusJSON {
			printStatusJSON(result)
		} else {
			printStatusTable(result)
		}
	},
}

func init() {
	StatusCmd.Flags().BoolVar(&statusJSON, "json", false, "Output status in JSON format")
}

func printStatusJSON(r statusResult) {
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error marshaling JSON: %v\n", err)
		return
	}
	fmt.Println(string(data))
}

func printStatusTable(r statusResult) {
	fmt.Println()

	// Registry connectivity
	if r.Registry.Reachable {
		printer.PrintSuccess(fmt.Sprintf("Registry: %s (v%s)", r.Registry.URL, r.Registry.Version))
	} else {
		printer.PrintError(fmt.Sprintf("Registry: %s (unreachable)", r.Registry.URL))
		fmt.Println()
		printer.PrintInfo("Start the daemon with: arctl daemon start")
		fmt.Println()
		return
	}

	// CLI info
	printer.PrintInfo(fmt.Sprintf("CLI: v%s (%s)", r.CLI.Version, r.CLI.GitCommit))
	fmt.Println()

	// Artifact table
	headers := []string{"Artifact", "Count"}
	rows := [][]string{
		{"MCP Servers", strconv.Itoa(r.Artifacts.MCPServers)},
		{"Agents", strconv.Itoa(r.Artifacts.Agents)},
		{"Skills", strconv.Itoa(r.Artifacts.Skills)},
		{"Prompts", strconv.Itoa(r.Artifacts.Prompts)},
		{"Deployments", strconv.Itoa(r.Deployments)},
	}
	if err := printer.PrintTable(headers, rows); err != nil {
		fmt.Fprintf(os.Stderr, "Error printing table: %v\n", err)
	}
	fmt.Println()
}

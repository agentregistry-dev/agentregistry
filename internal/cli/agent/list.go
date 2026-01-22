package agent

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/spf13/cobra"
)

var (
	listAll      bool
	listPageSize int
	outputFormat string
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List agents",
	Long:  `List agents that are published to the registry.`,
	RunE:  runList,
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	agents, err := apiClient.GetAgents()
	if err != nil {
		return fmt.Errorf("failed to get agents: %w", err)
	}

	deployedAgents, err := apiClient.GetDeployedServers()
	if err != nil {
		log.Printf("Warning: Failed to get deployed agents: %v", err)
		deployedAgents = nil
	}

	if len(agents) == 0 {
		fmt.Println("No agents available")
		return nil
	}

	// Handle different output formats
	switch outputFormat {
	case "json":
		return outputDataJson(agents)
	case "yaml":
		return outputDataYaml(agents)
	default:
		displayPaginatedAgents(agents, deployedAgents, listPageSize, listAll)
	}

	return nil
}

func displayPaginatedAgents(agents []*models.AgentResponse, deployedAgents []*client.DeploymentResponse, pageSize int, showAll bool) {
	total := len(agents)

	if showAll || total <= pageSize {
		printAgentsTable(agents, deployedAgents)
		return
	}

	reader := bufio.NewReader(os.Stdin)
	start := 0

	for start < total {
		end := min(start+pageSize, total)

		printAgentsTable(agents[start:end], deployedAgents)

		remaining := total - end
		if remaining > 0 {
			fmt.Printf("\nShowing %d-%d of %d agents. %d more available.\n", start+1, end, total, remaining)
			fmt.Print("Press Enter to continue, 'a' for all, or 'q' to quit: ")

			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Println("\nStopping pagination.")
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))

			switch response {
			case "a", "all":
				fmt.Println()
				printAgentsTable(agents[end:], deployedAgents)
				return
			case "q", "quit":
				fmt.Println()
				return
			default:
				start = end
				fmt.Println()
			}
		} else {
			fmt.Printf("\nShowing all %d agents.\n", total)
			return
		}
	}
}

func printAgentsTable(agents []*models.AgentResponse, deployedAgents []*client.DeploymentResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Name", "Version", "Framework", "Language", "Provider", "Model", "Deployed", "Published")

	deployedMap := make(map[string]*client.DeploymentResponse)
	for _, d := range deployedAgents {
		if d.ResourceType == "agent" {
			deployedMap[d.ServerName] = d
		}
	}

	for _, a := range agents {
		deployedStatus := "False"
		if deployment, ok := deployedMap[a.Agent.Name]; ok {
			if deployment.Version == a.Agent.Version {
				deployedStatus = "True"
			} else {
				deployedStatus = fmt.Sprintf("True (v%s)", deployment.Version)
			}
		}

		publishedStatus := "False"
		if a.Meta.Official.Published {
			publishedStatus = "True"
		}

		t.AddRow(
			printer.TruncateString(a.Agent.Name, 40),
			a.Agent.Version,
			printer.EmptyValueOrDefault(a.Agent.Framework, "<none>"),
			printer.EmptyValueOrDefault(a.Agent.Language, "<none>"),
			printer.EmptyValueOrDefault(a.Agent.ModelProvider, "<none>"),
			printer.TruncateString(printer.EmptyValueOrDefault(a.Agent.ModelName, "<none>"), 30),
			deployedStatus,
			publishedStatus,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

func outputDataJson(data any) error {
	p := printer.New(printer.OutputTypeJSON, false)
	if err := p.PrintJSON(data); err != nil {
		return fmt.Errorf("failed to output JSON: %w", err)
	}
	return nil
}

func outputDataYaml(data any) error {
	// For now, YAML is not implemented, fallback to JSON
	fmt.Println("YAML output not yet implemented, using JSON:")
	return outputDataJson(data)
}

func init() {
	ListCmd.Flags().BoolVarP(&listAll, "all", "a", false, "Show all items without pagination")
	ListCmd.Flags().IntVarP(&listPageSize, "page-size", "p", 15, "Number of items per page")
	ListCmd.Flags().StringVarP(&outputFormat, "output", "o", "table", "Output format (table, json, yaml)")
}

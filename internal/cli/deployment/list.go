package deployment

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var ListCmd = &cobra.Command{
	Use:   "list",
	Short: "List deployments",
	Long: `List all deployments (agents and MCP servers).

Example:
  arctl deployments list
  arctl deployments list --type agent
  arctl deployments list --type mcp
  arctl deployments list --status deployed`,
	Aliases:       []string{"ls"},
	RunE:          runList,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	ListCmd.Flags().String("type", "", "Filter by resource type (agent or mcp)")
	ListCmd.Flags().String("status", "", "Filter by deployment status (deploying, deployed, failed, cancelled, discovered)")
	ListCmd.Flags().String("provider", "", "Filter by provider ID")
	ListCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
}

func runList(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	typeFilter, _ := cmd.Flags().GetString("type")
	statusFilter, _ := cmd.Flags().GetString("status")
	providerFilter, _ := cmd.Flags().GetString("provider")
	outputFormat, _ := cmd.Flags().GetString("output")

	deployments, err := apiClient.GetDeployedServers()
	if err != nil {
		return fmt.Errorf("failed to get deployments: %w", err)
	}

	// Apply client-side filters
	filtered := filterDeployments(deployments, typeFilter, statusFilter, providerFilter)

	if len(filtered) == 0 {
		fmt.Println("No deployments found")
		return nil
	}

	// Redact env values to avoid leaking secrets (API keys, etc.)
	for _, d := range filtered {
		d.Env = redactEnv(d.Env)
	}

	switch outputFormat {
	case "json":
		p := printer.New(printer.OutputTypeJSON, false)
		return p.PrintJSON(filtered)
	default:
		printDeploymentsTable(filtered)
	}

	return nil
}

func filterDeployments(deployments []*client.DeploymentResponse, typeFilter, statusFilter, providerFilter string) []*client.DeploymentResponse {
	typeFilter = strings.ToLower(typeFilter)
	statusFilter = strings.ToLower(statusFilter)
	providerFilter = strings.ToLower(providerFilter)

	var filtered []*client.DeploymentResponse
	for _, d := range deployments {
		if typeFilter != "" && strings.ToLower(d.ResourceType) != typeFilter {
			continue
		}
		if statusFilter != "" && strings.ToLower(effectiveStatus(d)) != statusFilter {
			continue
		}
		if providerFilter != "" && strings.ToLower(d.ProviderID) != providerFilter {
			continue
		}
		filtered = append(filtered, d)
	}
	return filtered
}

func printDeploymentsTable(deployments []*client.DeploymentResponse) {
	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("ID", "Name", "Version", "Type", "Provider", "Status", "Age")

	for _, d := range deployments {
		age := ""
		if !d.DeployedAt.IsZero() {
			age = printer.FormatAge(d.DeployedAt)
		}

		id := truncateID(d.ID)
		status := effectiveStatus(d)
		if d.Origin == "discovered" {
			id = "-"
		}

		t.AddRow(
			id,
			d.ServerName,
			d.Version,
			d.ResourceType,
			d.ProviderID,
			status,
			age,
		)
	}

	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}
}

// effectiveStatus returns "discovered" when the deployment was discovered,
// otherwise returns the raw status. This keeps filtering consistent with display.
func effectiveStatus(d *client.DeploymentResponse) string {
	if d.Origin == "discovered" {
		return "discovered"
	}
	return d.Status
}

// truncateID shows the first 8 characters of a deployment ID for display.
func truncateID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

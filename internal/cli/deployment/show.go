package deployment

import (
	"fmt"
	"os"
	"slices"
	"strings"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	"github.com/spf13/cobra"
)

var ShowCmd = &cobra.Command{
	Use:   "show <deployment-id>",
	Short: "Show details of a deployment",
	Long: `Show detailed information about a deployment.

Example:
  arctl deployments show eb2d8231
  arctl deployments show eb2d8231-def6-7890-ghij-klmnopqrstuv`,
	Args:          cobra.ExactArgs(1),
	RunE:          runShow,
	SilenceUsage:  true,
	SilenceErrors: false,
}

func init() {
	ShowCmd.Flags().StringP("output", "o", "table", "Output format (table, json)")
}

func runShow(cmd *cobra.Command, args []string) error {
	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	outputFormat, _ := cmd.Flags().GetString("output")

	fullID, err := resolveDeploymentID(args[0])
	if err != nil {
		return err
	}

	dep, err := apiClient.GetDeployment(fullID)
	if err != nil {
		return fmt.Errorf("failed to get deployment: %w", err)
	}
	if dep == nil {
		return fmt.Errorf("deployment not found: %s", args[0])
	}

	// Redact env values to avoid leaking secrets (API keys, etc.)
	dep.Env = redactEnv(dep.Env)

	if outputFormat == "json" {
		p := printer.New(printer.OutputTypeJSON, false)
		return p.PrintJSON(dep)
	}

	t := printer.NewTablePrinter(os.Stdout)
	t.SetHeaders("Property", "Value")
	t.AddRow("ID", dep.ID)
	t.AddRow("Name", dep.ServerName)
	t.AddRow("Version", dep.Version)
	t.AddRow("Type", dep.ResourceType)
	t.AddRow("Status", dep.Status)
	t.AddRow("Origin", dep.Origin)
	t.AddRow("Provider", printer.EmptyValueOrDefault(dep.ProviderID, "<none>"))

	if dep.Error != "" {
		t.AddRow("Error", dep.Error)
	}

	if !dep.DeployedAt.IsZero() {
		t.AddRow("Deployed", dep.DeployedAt.Format("2006-01-02 15:04:05 MST"))
	}
	if !dep.UpdatedAt.IsZero() {
		t.AddRow("Updated", dep.UpdatedAt.Format("2006-01-02 15:04:05 MST"))
	}

	if len(dep.ProviderMetadata) > 0 {
		t.AddRow("Provider Metadata", formatMap(dep.ProviderMetadata))
	}

	if dep.ProviderID == "local" && dep.Status == "deployed" {
		switch dep.ResourceType {
		case "agent":
			t.AddRow("URL", fmt.Sprintf("http://localhost:%s/agents/%s-%s", cliCommon.DefaultAgentGatewayPort, dep.ServerName, dep.ID))
		case "mcp":
			t.AddRow("URL", fmt.Sprintf("http://localhost:%s/mcp", cliCommon.DefaultAgentGatewayPort))
		}
	}

	if len(dep.Env) > 0 {
		t.AddRow("Env Vars", fmt.Sprintf("%d configured", len(dep.Env)))
	}

	if err := t.Render(); err != nil {
		return fmt.Errorf("failed to render table: %w", err)
	}

	return nil
}

// redactEnv returns a copy of the env map with all values replaced by "***".
func redactEnv(env map[string]string) map[string]string {
	if len(env) == 0 {
		return env
	}
	redacted := make(map[string]string, len(env))
	for k := range env {
		redacted[k] = "***"
	}
	return redacted
}

func formatMap(m map[string]any) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, m[k]))
	}
	return strings.Join(parts, ", ")
}

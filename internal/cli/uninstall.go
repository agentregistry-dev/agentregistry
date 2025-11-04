package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/database"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/printer"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
)

var uninstallCmd = &cobra.Command{
	Use:   "uninstall <resource-type> <resource-name>",
	Short: "Uninstall a resource",
	Long:  `Uninstall resources (mcp server, skill).`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		resourceType := args[0]
		resourceName := args[1]

		// Initialize database
		if err := database.Initialize(); err != nil {
			log.Fatalf("Failed to initialize database: %v", err)
		}
		defer func() {
			if err := database.Close(); err != nil {
				log.Printf("Warning: Failed to close database: %v", err)
			}
		}()

		switch resourceType {
		case "mcp":
			if err := uninstallMCPServer(resourceName); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to uninstall MCP server: %v", err))
				return
			}
		case "skill":
			if err := uninstallSkill(resourceName); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to uninstall skill: %v", err))
				return
			}
		default:
			printer.PrintError(fmt.Sprintf("Unknown resource type: %s", resourceType))
			fmt.Println("Valid types: mcp, skill")
			return
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully uninstalled %s: %s", resourceType, resourceName))
	},
}

func uninstallMCPServer(serverName string) error {
	fmt.Printf("Uninstalling MCP server: %s\n", serverName)

	// Find servers using the same logic as install command
	servers := findServersByName(serverName)

	if len(servers) == 0 {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	if len(servers) > 1 {
		fmt.Printf("\nError: Multiple servers found matching '%s':\n", serverName)
		for _, s := range servers {
			fmt.Printf("  - %s (from registry: %s)\n", s.Name, s.RegistryName)
		}
		return fmt.Errorf("please use the full server name (namespace/name) to specify which server to uninstall")
	}

	// Exactly one match - proceed with uninstallation
	server := servers[0]

	if !server.Installed {
		return fmt.Errorf("server %s is not installed", server.Name)
	}

	// Mark as uninstalled in database
	if err := database.MarkServerInstalled(server.ID, false); err != nil {
		return fmt.Errorf("failed to mark server as uninstalled: %w", err)
	}

	// Get all remaining installed servers
	allServers, err := database.GetServers()
	if err != nil {
		return fmt.Errorf("failed to get servers: %w", err)
	}

	// Build list of MCPServerRunRequests for remaining installed servers
	var runRequests []*registry.MCPServerRunRequest
	for _, s := range allServers {
		if !s.Installed {
			continue
		}

		// Parse the server data
		var combinedData models.CombinedServerData
		if err := json.Unmarshal([]byte(s.Data), &combinedData); err != nil {
			return fmt.Errorf("failed to parse server data for %s: %w", s.Name, err)
		}

		// Re-marshal the server part to convert it to apiv0.ServerJSON format
		serverBytes, err := json.Marshal(combinedData.Server)
		if err != nil {
			return fmt.Errorf("failed to marshal server data for %s: %w", s.Name, err)
		}

		var registryServer apiv0.ServerJSON
		if err := json.Unmarshal(serverBytes, &registryServer); err != nil {
			return fmt.Errorf("failed to parse registry server for %s: %w", s.Name, err)
		}

		runRequests = append(runRequests, &registry.MCPServerRunRequest{
			RegistryServer: &registryServer,
			PreferRemote:   false,
			EnvValues:      make(map[string]string),
			ArgValues:      make(map[string]string),
			HeaderValues:   make(map[string]string),
		})
	}

	// Get the runtime directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	runtimeDir := filepath.Join(homeDir, ".arctl", "runtime")

	// Create runtime with translators
	regTranslator := registry.NewTranslator()
	composeTranslator := dockercompose.NewAgentGatewayTranslator(runtimeDir, 8080)
	agentRuntime := runtime.NewAgentRegistryRuntime(
		regTranslator,
		composeTranslator,
		runtimeDir,
		true, // verbose
	)

	// Reconcile with the remaining servers (this will remove the uninstalled server's containers)
	fmt.Printf("\nReconciling runtime state...\n")
	if err := agentRuntime.ReconcileMCPServers(context.Background(), runRequests); err != nil {
		return fmt.Errorf("failed to reconcile runtime: %w", err)
	}

	return nil
}

func uninstallSkill(skillName string) error {
	fmt.Printf("Uninstalling skill: %s\n", skillName)

	// Fetch skill from database
	skill, err := database.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to fetch skill: %w", err)
	}

	if skill == nil {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	if !skill.Installed {
		return fmt.Errorf("skill %s is not installed", skillName)
	}

	// Mark as uninstalled in database
	if err := database.MarkSkillInstalled(skill.ID, false); err != nil {
		return fmt.Errorf("failed to mark skill as uninstalled: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(uninstallCmd)
}

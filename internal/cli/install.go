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

var (
	preferRemote   bool
	installVersion string
	envVars        []string
	argVars        []string
	headerVars     []string
)

var installCmd = &cobra.Command{
	Use:   "install <resource-type> <resource-name>",
	Short: "Install a resource",
	Long:  `Install resources (mcp server, skill) from connected registries.`,
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
			if err := installMCPServer(resourceName); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to install MCP server: %v", err))
				return
			}
		case "skill":
			if err := installSkill(resourceName); err != nil {
				printer.PrintError(fmt.Sprintf("Failed to install skill: %v", err))
				return
			}
		default:
			printer.PrintError(fmt.Sprintf("Unknown resource type: %s", resourceType))
			fmt.Println("Valid types: mcp, skill")
			return
		}

		printer.PrintSuccess(fmt.Sprintf("Successfully installed %s: %s", resourceType, resourceName))
	},
}

func installMCPServer(serverName string) error {
	fmt.Printf("Installing MCP server: %s\n", serverName)

	// Find servers using the same logic as show command
	servers := findServersByName(serverName)

	if len(servers) == 0 {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	// Filter by version if specified
	if installVersion != "" {
		var filteredServers []*models.ServerDetail
		for _, s := range servers {
			if s.Version == installVersion {
				filteredServers = append(filteredServers, s)
			}
		}
		if len(filteredServers) == 0 {
			fmt.Printf("\nError: Server '%s' with version '%s' not found\n", serverName, installVersion)
			fmt.Printf("Available versions:\n")
			for _, s := range servers {
				fmt.Printf("  - %s\n", s.Version)
			}
			return fmt.Errorf("version '%s' not found", installVersion)
		}
		servers = filteredServers
	}

	// Check if multiple versions exist without version specification
	if len(servers) > 1 {
		// Group by name to check if these are different servers or same server with different versions
		nameMap := make(map[string][]*models.ServerDetail)
		for _, s := range servers {
			nameMap[s.Name] = append(nameMap[s.Name], s)
		}

		if len(nameMap) == 1 {
			// Same server, multiple versions
			fmt.Printf("\nError: Multiple versions of '%s' found:\n", serverName)
			for _, s := range servers {
				status := "available"
				if s.Installed {
					status = "installed"
				}
				fmt.Printf("  - %s (%s)\n", s.Version, status)
			}
			return fmt.Errorf("please specify a version using --version flag")
		} else {
			// Different servers with same partial name
			fmt.Printf("\nError: Multiple servers found matching '%s':\n", serverName)
			for _, s := range servers {
				fmt.Printf("  - %s (version: %s, from registry: %s)\n", s.Name, s.Version, s.RegistryName)
			}
			return fmt.Errorf("please use the full server name (namespace/name) to specify which server to install")
		}
	}

	// Exactly one match - proceed with installation
	server := servers[0]

	if server.Installed {
		return fmt.Errorf("server %s is already installed", server.Name)
	}

	// Parse the server data to get the full ServerJSON
	// The Data field contains a wrapper with _meta and server fields
	var combinedData models.CombinedServerData
	if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
		return fmt.Errorf("failed to parse server data: %w", err)
	}

	// Re-marshal the server part to convert it to apiv0.ServerJSON format
	serverBytes, err := json.Marshal(combinedData.Server)
	if err != nil {
		return fmt.Errorf("failed to marshal server data: %w", err)
	}

	var registryServer apiv0.ServerJSON
	if err := json.Unmarshal(serverBytes, &registryServer); err != nil {
		return fmt.Errorf("failed to parse registry server: %w", err)
	}

	// Parse environment variables, arguments, and headers from flags
	envValues, err := parseKeyValuePairs(envVars)
	if err != nil {
		return fmt.Errorf("failed to parse environment variables: %w", err)
	}

	argValues, err := parseKeyValuePairs(argVars)
	if err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}

	headerValues, err := parseKeyValuePairs(headerVars)
	if err != nil {
		return fmt.Errorf("failed to parse headers: %w", err)
	}

	// Create the run request
	runRequest := &registry.MCPServerRunRequest{
		RegistryServer: &registryServer,
		PreferRemote:   preferRemote,
		EnvValues:      envValues,
		ArgValues:      argValues,
		HeaderValues:   headerValues,
	}

	// Get the runtime directory from config or use default
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

	// Deploy the server using ReconcileMCPServers
	fmt.Printf("\nDeploying MCP server...\n")
	if err := agentRuntime.ReconcileMCPServers(context.Background(), []*registry.MCPServerRunRequest{runRequest}); err != nil {
		// Display server details when deployment fails
		fmt.Fprintf(os.Stderr, "\n")
		printer.PrintError("Installation failed. Server details:")
		fmt.Fprintf(os.Stderr, "\n")
		displayServerSummary(server)
		return fmt.Errorf("failed to deploy server: %w", err)
	}

	// Mark as installed in database
	if err := database.MarkServerInstalled(server.ID, true); err != nil {
		return fmt.Errorf("failed to mark server as installed: %w", err)
	}

	return nil
}

func installSkill(skillName string) error {
	fmt.Printf("Installing skill: %s\n", skillName)

	// Fetch skill from database (using exact match for now)
	skill, err := database.GetSkillByName(skillName)
	if err != nil {
		return fmt.Errorf("failed to fetch skill: %w", err)
	}

	if skill == nil {
		return fmt.Errorf("skill '%s' not found", skillName)
	}

	if skill.Installed {
		return fmt.Errorf("skill %s is already installed", skillName)
	}

	// Mark as installed in database
	if err := database.MarkSkillInstalled(skill.ID, true); err != nil {
		return fmt.Errorf("failed to mark skill as installed: %w", err)
	}

	return nil
}

// parseKeyValuePairs parses key=value pairs from command line flags
func parseKeyValuePairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, pair := range pairs {
		idx := findFirstEquals(pair)
		if idx == -1 {
			return nil, fmt.Errorf("invalid key=value pair (missing =): %s", pair)
		}
		key := pair[:idx]
		value := pair[idx+1:]
		result[key] = value
	}
	return result, nil
}

// findFirstEquals finds the first = character in a string
func findFirstEquals(s string) int {
	for i, c := range s {
		if c == '=' {
			return i
		}
	}
	return -1
}

// displayServerSummary displays a summary of server details for error messages
func displayServerSummary(server *models.ServerDetail) {
	// Parse the combined data to extract more details
	var combinedData models.CombinedServerData
	var registryType, registryStatus string
	var hasPackages, hasRemotes bool

	if err := json.Unmarshal([]byte(server.Data), &combinedData); err == nil {
		// Extract registry type
		if len(combinedData.Server.Packages) > 0 {
			registryType = combinedData.Server.Packages[0].RegistryType
			hasPackages = true
		}
		if len(combinedData.Server.Remotes) > 0 {
			if registryType == "" {
				registryType = combinedData.Server.Remotes[0].Type
			}
			hasRemotes = true
		}

		// Extract status
		registryStatus = combinedData.Meta.Official.Status
	}

	// Use installed status if registry status is not available
	if registryStatus == "" {
		if server.Installed {
			registryStatus = "installed"
		} else {
			registryStatus = "available"
		}
	}

	// Split namespace and name
	namespace, name := splitServerName(server.Name)

	// Display server details in table format
	t := printer.NewTablePrinter(os.Stderr)
	t.SetHeaders("Property", "Value")
	t.AddRow("Full Name", server.Name)
	t.AddRow("Namespace", printer.EmptyValueOrDefault(namespace, "<none>"))
	t.AddRow("Name", name)
	t.AddRow("Description", printer.EmptyValueOrDefault(server.Description, "<none>"))
	t.AddRow("Version", server.Version)
	t.AddRow("Type", printer.EmptyValueOrDefault(registryType, "<none>"))
	t.AddRow("Status", registryStatus)
	t.AddRow("Registry", server.RegistryName)
	t.AddRow("Has Packages", fmt.Sprintf("%v", hasPackages))
	t.AddRow("Has Remotes", fmt.Sprintf("%v", hasRemotes))
	if err := t.Render(); err != nil {
		printer.PrintError(fmt.Sprintf("failed to render table: %v", err))
	}

	// Show suggestion based on what's available
	fmt.Fprintf(os.Stderr, "\n")
	if !hasPackages && !hasRemotes {
		printer.PrintWarning("This server has no packages or remotes configured")
	} else if hasPackages && !hasRemotes {
		printer.PrintInfo("This server has packages but no remotes. Try without --prefer-remote flag")
	} else if hasRemotes && !hasPackages {
		printer.PrintInfo("This server has remotes but no packages. Try with --prefer-remote flag")
	} else {
		printer.PrintInfo("This server has both packages and remotes available")
	}
}

func init() {
	rootCmd.AddCommand(installCmd)
	installCmd.Flags().BoolVar(&preferRemote, "prefer-remote", false, "Prefer remote deployment over local packages")
	installCmd.Flags().StringVarP(&installVersion, "version", "v", "", "Install specific version of the server")
	installCmd.Flags().StringArrayVarP(&envVars, "env", "e", []string{}, "Environment variables (key=value)")
	installCmd.Flags().StringArrayVarP(&argVars, "arg", "", []string{}, "Arguments (key=value)")
	installCmd.Flags().StringArrayVar(&headerVars, "header", []string{}, "Headers for remote servers (key=value)")
}

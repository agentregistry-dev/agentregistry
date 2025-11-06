package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
)

var (
	runVersion    string
	runInspector  bool
	runYes        bool
	runVerbose    bool
	runEnvVars    []string
	runArgVars    []string
	runHeaderVars []string
)

var runCmd = &cobra.Command{
	Use:   "run <resource-type> <resource-name>",
	Short: "Run a resource",
	Long:  `Runs a resource (agent, mcp).`,
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		resourceType := args[0]
		if resourceType != "agent" && resourceType != "mcp" {
			fmt.Println("Invalid resource type")
			return
		}

		resourceName := args[1]
		switch resourceType {
		case "agent":
			fmt.Println("Not implemented yet")
			return
		case "mcp":
			runMCPServer(resourceName)
		default:
			fmt.Println("Invalid resource type")
			return
		}
	},
}

func init() {
	rootCmd.AddCommand(runCmd)
	runCmd.Flags().StringVarP(&runVersion, "version", "v", "", "Specify the version of the resource to run")
	runCmd.Flags().BoolVar(&runInspector, "inspector", false, "Launch MCP Inspector to interact with the server")
	runCmd.Flags().BoolVarP(&runYes, "yes", "y", false, "Automatically accept all prompts (use default values)")
	runCmd.Flags().BoolVar(&runVerbose, "verbose", false, "Enable verbose logging")
	runCmd.Flags().StringArrayVarP(&runEnvVars, "env", "e", []string{}, "Environment variables (key=value)")
	runCmd.Flags().StringArrayVar(&runArgVars, "arg", []string{}, "Runtime arguments (key=value)")
	runCmd.Flags().StringArrayVar(&runHeaderVars, "header", []string{}, "Headers for remote servers (key=value)")
}

func runMCPServer(resourceName string) {
	if APIClient == nil {
		fmt.Println("Error: API client not initialized")
		return
	}

	// If a specific version was requested, try to get that version
	if runVersion != "" {
		fmt.Printf("Checking if MCP server '%s' version '%s' exists in registry...\n", resourceName, runVersion)
		server, err := APIClient.GetServerByNameAndVersion(resourceName, runVersion)
		if err != nil {
			fmt.Printf("Error querying registry: %v\n", err)
			return
		}

		if server == nil {
			fmt.Printf("Error: MCP server '%s' version '%s' not found in registry\n", resourceName, runVersion)
			fmt.Println("Use 'arctl list mcp' to see available servers and versions")
			return
		}

		// Server version found, proceed with running it
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", server.Title, server.Version)
		if err := runMCPServerWithRuntime(server); err != nil {
			fmt.Printf("Error running MCP server: %v\n", err)
			return
		}
		return
	}

	// No specific version requested, check if server exists and handle multiple versions
	fmt.Printf("Checking if MCP server '%s' exists in registry...\n", resourceName)
	versions, err := APIClient.GetServerVersions(resourceName)
	if err != nil {
		fmt.Printf("Error querying registry: %v\n", err)
		return
	}

	// Check if server was found
	if len(versions) == 0 {
		fmt.Printf("Error: MCP server '%s' not found in registry\n", resourceName)
		fmt.Println("Use 'arctl list mcp' to see available servers")
		return
	}

	// Get the latest version (first in the list, as they're ordered by date)
	latestServer := versions[0]

	// If there are multiple versions, prompt the user (unless --yes is set)
	if len(versions) > 1 {
		fmt.Printf("✓ Found %d versions of MCP server '%s':\n", len(versions), resourceName)
		for i, v := range versions {
			marker := ""
			if i == 0 {
				marker = " (latest)"
			}
			fmt.Printf("  - %s%s\n", v.Version, marker)
		}
		fmt.Printf("\nDefault: version %s (latest)\n", latestServer.Version)

		// Skip prompt if --yes flag is set
		if !runYes {
			// Prompt user for confirmation
			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Proceed with the latest version? [Y/n]: ")
			response, err := reader.ReadString('\n')
			if err != nil {
				fmt.Printf("Error reading input: %v\n", err)
				return
			}

			response = strings.TrimSpace(strings.ToLower(response))
			if response != "" && response != "y" && response != "yes" {
				fmt.Println("Operation cancelled.")
				fmt.Printf("To run a specific version, use: arctl run mcp %s --version <version>\n", resourceName)
				return
			}
		} else {
			fmt.Println("Auto-accepting latest version (--yes flag set)")
		}
	} else {
		// Only one version available
		fmt.Printf("✓ Found MCP server: %s (version %s)\n", latestServer.Title, latestServer.Version)
	}

	// Proceed with running the latest version
	if err := runMCPServerWithRuntime(&latestServer); err != nil {
		fmt.Printf("Error running MCP server: %v\n", err)
		return
	}
}

// runMCPServerWithRuntime starts an MCP server using the runtime
func runMCPServerWithRuntime(server *models.ServerDetail) error {
	var combinedData models.CombinedServerData
	if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
		return fmt.Errorf("failed to parse server data: %w", err)
	}

	serverBytes, err := json.Marshal(combinedData.Server)
	if err != nil {
		return fmt.Errorf("failed to marshal server data: %w", err)
	}

	var registryServer apiv0.ServerJSON
	if err := json.Unmarshal(serverBytes, &registryServer); err != nil {
		return fmt.Errorf("failed to parse registry server: %w", err)
	}

	// Parse environment variables, arguments, and headers from flags
	envValues, err := parseKeyValuePairs(runEnvVars)
	if err != nil {
		return fmt.Errorf("failed to parse environment variables: %w", err)
	}

	argValues, err := parseKeyValuePairs(runArgVars)
	if err != nil {
		return fmt.Errorf("failed to parse arguments: %w", err)
	}

	headerValues, err := parseKeyValuePairs(runHeaderVars)
	if err != nil {
		return fmt.Errorf("failed to parse headers: %w", err)
	}

	runRequest := &registry.MCPServerRunRequest{
		RegistryServer: &registryServer,
		PreferRemote:   false,
		EnvValues:      envValues,
		ArgValues:      argValues,
		HeaderValues:   headerValues,
	}

	// Generate a random runtime directory name and project name
	projectName, runtimeDir, err := generateRuntimePaths("arctl-run-")
	if err != nil {
		return err
	}

	// Find an available port for the agent gateway
	agentGatewayPort, err := findAvailablePort()
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	// Create runtime with translators
	regTranslator := registry.NewTranslator()
	composeTranslator := dockercompose.NewAgentGatewayTranslatorWithProjectName(runtimeDir, agentGatewayPort, projectName)
	agentRuntime := runtime.NewAgentRegistryRuntime(
		regTranslator,
		composeTranslator,
		runtimeDir,
		runVerbose,
	)

	fmt.Printf("Starting MCP server: %s (version %s)...\n", server.Name, server.Version)

	// Start the server
	if err := agentRuntime.ReconcileMCPServers(context.Background(), []*registry.MCPServerRunRequest{runRequest}); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	agentGatewayURL := fmt.Sprintf("http://localhost:%d/mcp", agentGatewayPort)
	fmt.Printf("\nAgent Gateway endpoint: %s\n", agentGatewayURL)

	// Launch inspector if requested
	var inspectorCmd *exec.Cmd
	if runInspector {
		fmt.Println("\nLaunching MCP Inspector...")
		inspectorCmd = exec.Command("npx", "-y", "@modelcontextprotocol/inspector", "--server-url", agentGatewayURL)
		inspectorCmd.Stdout = os.Stdout
		inspectorCmd.Stderr = os.Stderr
		inspectorCmd.Stdin = os.Stdin

		if err := inspectorCmd.Start(); err != nil {
			fmt.Printf("Warning: Failed to start MCP Inspector: %v\n", err)
			fmt.Println("You can manually run: npx @modelcontextprotocol/inspector --server-url " + agentGatewayURL)
			inspectorCmd = nil
		} else {
			fmt.Println("✓ MCP Inspector launched")
		}
	}

	fmt.Println("\nPress CTRL+C to stop the server and clean up...")
	return waitForShutdown(runtimeDir, projectName, inspectorCmd)
}

// findAvailablePort finds an available port on localhost
func findAvailablePort() (uint16, error) {
	// Try to bind to port 0, which tells the OS to pick an available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer func() { _ = listener.Close() }()

	// Get the port that was assigned
	addr := listener.Addr().(*net.TCPAddr)
	return uint16(addr.Port), nil
}

// waitForShutdown waits for CTRL+C and then cleans up
func waitForShutdown(runtimeDir, projectName string, inspectorCmd *exec.Cmd) error {
	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Block until we receive a signal
	<-sigChan

	fmt.Println("\n\nReceived shutdown signal, cleaning up...")

	// Stop the inspector if it's running
	if inspectorCmd != nil && inspectorCmd.Process != nil {
		fmt.Println("Stopping MCP Inspector...")
		if err := inspectorCmd.Process.Kill(); err != nil {
			fmt.Printf("Warning: Failed to stop MCP Inspector: %v\n", err)
		} else {
			// Wait for the process to exit
			_ = inspectorCmd.Wait()
			fmt.Println("✓ MCP Inspector stopped")
		}
	}

	// Stop the docker compose services
	fmt.Println("Stopping Docker containers...")
	stopCmd := exec.Command("docker", "compose", "-p", projectName, "down")
	stopCmd.Dir = runtimeDir
	stopCmd.Stdout = os.Stdout
	stopCmd.Stderr = os.Stderr
	if err := stopCmd.Run(); err != nil {
		fmt.Printf("Warning: Failed to stop Docker containers: %v\n", err)
		// Continue with cleanup even if docker compose down fails
	} else {
		fmt.Println("✓ Docker containers stopped")
	}

	// Remove the temporary runtime directory
	fmt.Printf("Removing runtime directory: %s\n", runtimeDir)
	if err := os.RemoveAll(runtimeDir); err != nil {
		fmt.Printf("Warning: Failed to remove runtime directory: %v\n", err)
		return fmt.Errorf("cleanup incomplete: %w", err)
	}
	fmt.Println("✓ Runtime directory removed")

	fmt.Println("\n✓ Cleanup completed successfully")
	return nil
}

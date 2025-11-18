package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

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

var RunCmd = &cobra.Command{
	Use:   "run <server-name>",
	Short: "Run an MCP server",
	Long:  `Run an MCP server locally.`,
	Args:  cobra.ExactArgs(1),
	RunE:  runRun,
}

func init() {
	RunCmd.Flags().StringVarP(&runVersion, "version", "v", "", "Specify the version of the server to run")
	RunCmd.Flags().BoolVar(&runInspector, "inspector", false, "Launch MCP Inspector to interact with the server")
	RunCmd.Flags().BoolVarP(&runYes, "yes", "y", false, "Automatically accept all prompts (use default values)")
	RunCmd.Flags().BoolVar(&runVerbose, "verbose", false, "Enable verbose logging")
	RunCmd.Flags().StringArrayVarP(&runEnvVars, "env", "e", []string{}, "Environment variables (key=value)")
	RunCmd.Flags().StringArrayVar(&runArgVars, "arg", []string{}, "Runtime arguments (key=value)")
	RunCmd.Flags().StringArrayVar(&runHeaderVars, "header", []string{}, "Headers for remote servers (key=value)")
}

func runRun(cmd *cobra.Command, args []string) error {
	serverName := args[0]

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Use the common server version selection logic
	server, err := selectServerVersion(serverName, runVersion, runYes)
	if err != nil {
		return err
	}

	// Proceed with running the server
	if err := runMCPServerWithRuntime(server); err != nil {
		return fmt.Errorf("error running MCP server: %w", err)
	}

	return nil
}

// runMCPServerWithRuntime starts an MCP server using the runtime
func runMCPServerWithRuntime(server *apiv0.ServerResponse) error {

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
		RegistryServer: &server.Server,
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

	fmt.Printf("Starting MCP server: %s (version %s)...\n", server.Server.Name, server.Server.Version)

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

// generateRandomName generates a random hex string for use in naming
func generateRandomName() (string, error) {
	randomBytes := make([]byte, 8)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("failed to generate random name: %w", err)
	}
	return hex.EncodeToString(randomBytes), nil
}

// generateRuntimePaths generates random names and paths for runtime directories
// Returns projectName, runtimeDir, and any error encountered
func generateRuntimePaths(prefix string) (projectName string, runtimeDir string, err error) {
	// Generate a random name
	randomName, err := generateRandomName()
	if err != nil {
		return "", "", err
	}

	// Create project name with prefix
	projectName = prefix + randomName

	// Get home directory and construct runtime directory path
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", "", fmt.Errorf("failed to get home directory: %w", err)
	}
	baseRuntimeDir := filepath.Join(homeDir, ".arctl", "runtime")
	runtimeDir = filepath.Join(baseRuntimeDir, prefix+randomName)

	return projectName, runtimeDir, nil
}

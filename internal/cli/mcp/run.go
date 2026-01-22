package mcp

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/agentregistry-dev/agentregistry/internal/cli/mcp/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/spf13/cobra"
	"github.com/stoewer/go-strcase"
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
	Use:   "run <server-name|path>",
	Short: "Run an MCP server",
	Long: `Run an MCP server locally.

You can run either:
  - A server from the registry by name (e.g., 'arctl mcp run @modelcontextprotocol/server-everything')
  - A local MCP project by path (e.g., 'arctl mcp run .' or 'arctl mcp run ./my-mcp-server')

For local projects, the server must be built first using 'arctl mcp build'.`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
}

func init() {
	RunCmd.Flags().StringVar(&runVersion, "version", "", "Specify the version of the server to run")
	RunCmd.Flags().BoolVar(&runInspector, "inspector", false, "Launch MCP Inspector to interact with the server")
	RunCmd.Flags().BoolVarP(&runYes, "yes", "y", false, "Automatically accept all prompts (use default values)")
	RunCmd.Flags().BoolVar(&runVerbose, "verbose", false, "Enable verbose logging")
	RunCmd.Flags().StringArrayVarP(&runEnvVars, "env", "e", []string{}, "Environment variables (key=value)")
	RunCmd.Flags().StringArrayVar(&runArgVars, "arg", []string{}, "Runtime arguments (key=value)")
	RunCmd.Flags().StringArrayVar(&runHeaderVars, "header", []string{}, "Headers for remote servers (key=value)")
}

func runRun(cmd *cobra.Command, args []string) error {
	serverNameOrPath := args[0]

	if isLocalPath(serverNameOrPath) {
		return runLocalMCPServer(serverNameOrPath)
	}

	if apiClient == nil {
		return fmt.Errorf("API client not initialized")
	}

	// Use the common server version selection logic
	server, err := selectServerVersion(serverNameOrPath, runVersion, runYes)
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
	agentGatewayPort, err := utils.FindAvailablePort()
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
	if err := agentRuntime.ReconcileAll(context.Background(), []*registry.MCPServerRunRequest{runRequest}, nil); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	agentGatewayURL := fmt.Sprintf("http://localhost:%d/mcp", agentGatewayPort)
	fmt.Printf("\nAgent Gateway endpoint: %s\n", agentGatewayURL)

	// Launch inspector if requested
	var inspectorCmd *exec.Cmd
	if runInspector {
		// Check if npx is installed
		_, err := exec.LookPath("npx")
		if err != nil {
			return fmt.Errorf("'npx' not found in PATH")
		}
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

// isLocalPath checks if the given string is a path to a local directory
func isLocalPath(path string) bool {
	// Check for common path indicators
	if path == "." || path == ".." || filepath.IsAbs(path) {
		return true
	}
	// Check for relative paths
	if len(path) > 0 && (path[0] == '.' || path[0] == '/') {
		return true
	}
	// Check if the path exists as a directory
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return true
	}
	return false
}

// runLocalMCPServer runs a local MCP server from a project directory
func runLocalMCPServer(projectPath string) error {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("failed to get absolute path: %w", err)
	}

	// Load the manifest
	manifestManager := manifest.NewManager(absPath)
	if !manifestManager.Exists() {
		return fmt.Errorf("mcp.yaml not found in %s. Run 'arctl mcp init' first", absPath)
	}

	projectManifest, err := manifestManager.Load()
	if err != nil {
		return fmt.Errorf("failed to load project manifest: %w", err)
	}

	// Determine the Docker image name (same logic as build command)
	version := projectManifest.Version
	if version == "" {
		version = "latest"
	}
	imageName := fmt.Sprintf("%s:%s", strcase.KebabCase(projectManifest.Name), version)

	// Check if the image exists locally
	if err := checkDockerImageExists(imageName); err != nil {
		return fmt.Errorf("docker image %s not found. Run 'arctl mcp build %s' first\n%w", imageName, projectPath, err)
	}

	fmt.Printf("Running local MCP server: %s (version %s)\n", projectManifest.Name, version)
	fmt.Printf("Using Docker image: %s\n", imageName)

	return runLocalMCPServerWithDocker(projectManifest, imageName)
}

// runLocalMCPServerWithDocker runs the Docker container directly for local development
func runLocalMCPServerWithDocker(manifest *manifest.ProjectManifest, imageName string) error {
	port, err := utils.FindAvailablePort()
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	// Parse environment variables from flags and merge with defaults
	envValues, err := parseKeyValuePairs(runEnvVars)
	if err != nil {
		return fmt.Errorf("failed to parse environment variables: %w", err)
	}

	if envValues["MCP_TRANSPORT_MODE"] == "" {
		envValues["MCP_TRANSPORT_MODE"] = "http"
	}
	if envValues["PORT"] == "" {
		envValues["PORT"] = "3000"
	}
	if envValues["HOST"] == "" {
		// Bind to 0.0.0.0 so the server is accessible from outside the container
		envValues["HOST"] = "0.0.0.0"
	}

	// Build docker run command
	containerName := fmt.Sprintf("arctl-run-%s", manifest.Name)
	args := []string{
		"run",
		"--rm",
		"--name", containerName,
		"-p", fmt.Sprintf("%d:3000", port),
	}

	for k, v := range envValues {
		args = append(args, "-e", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, imageName)

	fmt.Printf("\nMCP Server URL: http://localhost:%d/mcp\n", port)
	fmt.Println("\nPress CTRL+C to stop the server...")
	fmt.Println()

	// Create the docker run command
	dockerCmd := exec.Command("docker", args...)
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr
	dockerCmd.Stdin = os.Stdin

	// Start the container
	if err := dockerCmd.Start(); err != nil {
		return fmt.Errorf("failed to start docker container: %w", err)
	}

	// Launch inspector if requested
	var inspectorCmd *exec.Cmd
	if runInspector {
		serverURL := fmt.Sprintf("http://localhost:%d/mcp", port)
		fmt.Println("Launching MCP Inspector...")
		inspectorCmd = exec.Command("npx", "-y", "@modelcontextprotocol/inspector", "--server-url", serverURL)
		inspectorCmd.Stdout = os.Stdout
		inspectorCmd.Stderr = os.Stderr
		inspectorCmd.Stdin = os.Stdin

		if err := inspectorCmd.Start(); err != nil {
			fmt.Printf("Warning: Failed to start MCP Inspector: %v\n", err)
			fmt.Println("You can manually run: npx @modelcontextprotocol/inspector --server-url " + serverURL)
			inspectorCmd = nil
		} else {
			fmt.Println("✓ MCP Inspector launched")
		}
	}
	return waitForDockerContainer(dockerCmd, containerName, inspectorCmd)
}

// waitForDockerContainer waits for the docker container to finish or handles CTRL+C
func waitForDockerContainer(dockerCmd *exec.Cmd, containerName string, inspectorCmd *exec.Cmd) error {
	// Create a channel to receive OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	// Create a channel to wait for the docker command to finish
	doneChan := make(chan error, 1)
	go func() {
		doneChan <- dockerCmd.Wait()
	}()

	// Wait for either signal or docker command to finish
	select {
	case <-sigChan:
		fmt.Println("\n\nReceived shutdown signal, stopping container...")

		// Stop the inspector if it's running
		if inspectorCmd != nil && inspectorCmd.Process != nil {
			fmt.Println("Stopping MCP Inspector...")
			if err := inspectorCmd.Process.Kill(); err != nil {
				fmt.Printf("Warning: Failed to stop MCP Inspector: %v\n", err)
			} else {
				_ = inspectorCmd.Wait()
				fmt.Println("✓ MCP Inspector stopped")
			}
		}

		// Stop the container
		fmt.Println("Stopping Docker container...")
		stopCmd := exec.Command("docker", "stop", containerName)
		if err := stopCmd.Run(); err != nil {
			fmt.Printf("Warning: Failed to stop container: %v\n", err)
		} else {
			fmt.Println("✓ Docker container stopped")
		}

		// Wait for the docker command to finish
		<-doneChan
		fmt.Println("\n✓ Cleanup completed successfully")
		return nil

	case err := <-doneChan:
		// Container exited on its own
		if inspectorCmd != nil && inspectorCmd.Process != nil {
			_ = inspectorCmd.Process.Kill()
			_ = inspectorCmd.Wait()
		}
		if err != nil {
			return fmt.Errorf("docker container exited with error: %w", err)
		}
		return nil
	}
}

// checkDockerImageExists verifies that a Docker image exists locally
func checkDockerImageExists(imageName string) error {
	cmd := exec.Command("docker", "image", "inspect", imageName)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("image not found. run `arctl mcp build %s` to build the image", imageName)
	}
	return nil
}

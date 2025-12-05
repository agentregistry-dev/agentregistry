package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/spf13/cobra"
	a2aclient "trpc.group/trpc-go/trpc-a2a-go/client"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

var RunCmd = &cobra.Command{
	Use:   "run [project-directory-or-agent-name]",
	Short: "Run an agent locally and launch the interactive chat",
	Long: `Run an agent project locally via docker compose. If the argument is a directory,
arctl uses the local files; otherwise it fetches the agent by name from the registry and
launches the same chat interface.`,
	Args: cobra.ExactArgs(1),
	RunE: runRun,
	Example: `arctl agent run ./my-agent
  arctl agent run dice`,
}

var providerAPIKeys = map[string]string{
	"openai":      "OPENAI_API_KEY",
	"anthropic":   "ANTHROPIC_API_KEY",
	"azureopenai": "AZUREOPENAI_API_KEY",
}

func runRun(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}

	target := args[0]
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		fmt.Println("Running agent from local directory:", target)
		return runFromDirectory(cmd.Context(), target)
	}

	agentModel, err := apiClient.GetAgentByName(target)
	if err != nil {
		return fmt.Errorf("failed to resolve agent %q: %w", target, err)
	}
	manifest := agentModel.Agent.AgentManifest
	version := agentModel.Agent.Version
	return runFromManifest(cmd.Context(), &manifest, version, nil)
}

// Note: The below implementation may be redundant in most cases.
// It allows for registry-type MCP server resolution at run-time, but in doing so, it regenerates folders for servers which were already accounted for (i.e. command-type get generated during their `add-cmd` command)
// This is not a major issue or breaking, but something we could improve in the future.
func runFromDirectory(ctx context.Context, projectDir string) error {
	manifest, err := project.LoadManifest(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load agent.yaml: %w", err)
	}

	// Resolve registry-type MCP servers if present
	if hasRegistryServers(manifest) {
		// resolve registry-type MCP servers and add them to the manifest
		servers, err := agentutils.ParseAgentManifestServers(manifest, verbose)
		if err != nil {
			return fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
		}
		manifest.McpServers = servers

		var registryResolvedServers []common.McpServerType
		for _, srv := range manifest.McpServers {
			if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
				registryResolvedServers = append(registryResolvedServers, srv)
			}
		}
		if len(registryResolvedServers) > 0 {
			tmpManifest := *manifest
			tmpManifest.McpServers = registryResolvedServers
			// create directories and build images for the registry-resolved servers
			if err := project.EnsureMcpServerDirectories(projectDir, &tmpManifest, verbose); err != nil {
				return fmt.Errorf("failed to create MCP server directories: %w", err)
			}
			if err := writeResolvedMCPServerConfig(projectDir, &tmpManifest, "", verbose); err != nil {
				return fmt.Errorf("failed to write MCP server config: %w", err)
			}
		}
	} else {
		// no registry-type MCP servers, we clean up the resolved MCP server config in case any previous one exists.
		if err := cleanupResolvedMCPServerConfig(projectDir, manifest.Name, "", verbose); err != nil {
			return fmt.Errorf("failed to cleanup resolved MCP server config: %w", err)
		}
	}

	if err := project.RegenerateDockerCompose(projectDir, manifest, "", verbose); err != nil {
		return fmt.Errorf("failed to refresh docker-compose.yaml: %w", err)
	}

	composePath := filepath.Join(projectDir, "docker-compose.yaml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read docker-compose.yaml: %w", err)
	}

	return runFromManifest(ctx, manifest, "", &runContext{
		composeData: data,
		workDir:     projectDir,
	})
}

// hasRegistryServers checks if the manifest has any registry-type MCP servers.
func hasRegistryServers(manifest *common.AgentManifest) bool {
	for _, srv := range manifest.McpServers {
		if srv.Type == "registry" {
			return true
		}
	}
	return false
}

func runFromManifest(ctx context.Context, manifest *common.AgentManifest, version string, overrides *runContext) error {
	if manifest == nil {
		return fmt.Errorf("agent manifest is required")
	}

	var composeData []byte
	workDir := ""

	if overrides != nil {
		// servers already resolved, compose already generated (i.e. from runFromDirectory)
		composeData = overrides.composeData
		workDir = overrides.workDir
	} else {
		// we'll need to resolve and build registry-resolved MCP servers at runtime
		// if no registry-type MCP servers are present, we can skip this
		if hasRegistryServers(manifest) {
			tmpDir, err := os.MkdirTemp("", "arctl-registry-resolve-*")
			if err != nil {
				return fmt.Errorf("failed to create temporary directory: %w", err)
			}

			// Called with registry agent name - need to resolve and render in-memory
			servers, err := agentutils.ParseAgentManifestServers(manifest, verbose)
			if err != nil {
				return fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
			}
			manifest.McpServers = servers

			// filter by registry-resolved servers
			var registryResolvedServers []common.McpServerType
			for _, srv := range manifest.McpServers {
				if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
					registryResolvedServers = append(registryResolvedServers, srv)
				}
			}

			if len(registryResolvedServers) > 0 {
				// create a new manifest with only the registry-resolved servers to build
				tmpManifest := *manifest
				tmpManifest.McpServers = registryResolvedServers

				if err := project.EnsureMcpServerDirectories(tmpDir, &tmpManifest, verbose); err != nil {
					return fmt.Errorf("failed to create mcp server directories: %w", err)
				}
				if err := buildRegistryResolvedServers(tmpDir, &tmpManifest, verbose); err != nil {
					return fmt.Errorf("failed to build registry server images: %w", err)
				}
				if err := writeResolvedMCPServerConfig(tmpDir, &tmpManifest, version, verbose); err != nil {
					return fmt.Errorf("failed to write MCP server config: %w", err)
				}

				workDir = tmpDir
			}
		}

		data, err := renderComposeFromManifest(manifest, version)
		if err != nil {
			return err
		}
		composeData = data
	}

	err := runAgent(ctx, composeData, manifest, workDir)

	// Clean up temp directory for registry-run agents
	if workDir != "" && strings.Contains(workDir, "arctl-registry-resolve-") {
		if cleanupErr := os.RemoveAll(workDir); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary directory %s: %v\n", workDir, cleanupErr)
		}
	}

	return err
}

type runContext struct {
	composeData []byte
	workDir     string
}

func renderComposeFromManifest(manifest *common.AgentManifest, version string) ([]byte, error) {
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	image := manifest.Image
	if image == "" {
		image = project.ConstructImageName("", manifest.Name)
	}

	// Sanitize version for filesystem use in template
	sanitizedVersion := utils.SanitizeVersion(version)

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name          string
		Version       string
		Image         string
		ModelProvider string
		ModelName     string
		EnvVars       []string
		McpServers    []common.McpServerType
	}{
		Name:          manifest.Name,
		Version:       sanitizedVersion,
		Image:         image,
		ModelProvider: manifest.ModelProvider,
		ModelName:     manifest.ModelName,
		EnvVars:       project.EnvVarsFromManifest(manifest),
		McpServers:    manifest.McpServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render docker-compose template: %w", err)
	}
	return []byte(rendered), nil
}

func runAgent(ctx context.Context, composeData []byte, manifest *common.AgentManifest, workDir string) error {
	if err := validateAPIKey(manifest.ModelProvider); err != nil {
		return err
	}

	composeCmd := docker.ComposeCommand()
	commonArgs := append(composeCmd[1:], "-f", "-")

	upCmd := exec.CommandContext(ctx, composeCmd[0], append(commonArgs, "up", "-d")...)
	upCmd.Dir = workDir
	upCmd.Stdin = bytes.NewReader(composeData)
	if verbose {
		upCmd.Stdout = os.Stdout
		upCmd.Stderr = os.Stderr
	}

	if err := upCmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Println("✓ Docker containers started")

	time.Sleep(2 * time.Second)
	fmt.Println("Waiting for agent to be ready...")

	if err := waitForAgent(ctx, "http://localhost:8080", 60*time.Second); err != nil {
		printComposeLogs(composeCmd, commonArgs, composeData, workDir)
		return err
	}

	fmt.Printf("✓ Agent '%s' is running at http://localhost:8080\n", manifest.Name)

	if err := launchChat(ctx, manifest.Name); err != nil {
		return err
	}

	fmt.Println("\nStopping docker compose...")
	downCmd := exec.Command(composeCmd[0], append(commonArgs, "down")...)
	downCmd.Dir = workDir
	downCmd.Stdin = bytes.NewReader(composeData)
	if verbose {
		downCmd.Stdout = os.Stdout
		downCmd.Stderr = os.Stderr
	}
	if err := downCmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to stop docker compose: %v\n", err)
	} else {
		fmt.Println("✓ Stopped docker compose")
	}

	return nil
}

func waitForAgent(ctx context.Context, agentURL string, timeout time.Duration) error {
	healthURL := agentURL + "/health"
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Second}
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	fmt.Print("Checking agent health")
	for {
		select {
		case <-ctx.Done():
			fmt.Println()
			return fmt.Errorf("timeout waiting for agent to be ready")
		case <-ticker.C:
			fmt.Print(".")
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL, nil)
			if err != nil {
				continue
			}
			resp, err := client.Do(req)
			if err == nil {
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				if resp.StatusCode == http.StatusOK {
					fmt.Println(" ✓")
					return nil
				}
			}
		}
	}
}

func printComposeLogs(composeCmd []string, commonArgs []string, composeData []byte, workDir string) {
	fmt.Fprintln(os.Stderr, "Agent failed to start. Fetching logs...")
	logsCmd := exec.Command(composeCmd[0], append(commonArgs, "logs", "--tail=50")...)
	logsCmd.Dir = workDir
	logsCmd.Stdin = bytes.NewReader(composeData)
	output, err := logsCmd.CombinedOutput()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to fetch docker compose logs: %v\n", err)
		return
	}
	fmt.Fprintf(os.Stderr, "Container logs:\n%s\n", string(output))
}

func launchChat(ctx context.Context, agentName string) error {
	sessionID := protocol.GenerateContextID()
	client, err := a2aclient.NewA2AClient("http://localhost:8080", a2aclient.WithTimeout(60*time.Second))
	if err != nil {
		return fmt.Errorf("failed to create chat client: %w", err)
	}

	sendFn := func(ctx context.Context, params protocol.SendMessageParams) (<-chan protocol.StreamingMessageEvent, error) {
		ch, err := client.StreamMessage(ctx, params)
		if err != nil {
			return nil, err
		}
		return ch, nil
	}

	return tui.RunChat(agentName, sessionID, sendFn, verbose)
}

func validateAPIKey(modelProvider string) error {
	envVar, ok := providerAPIKeys[strings.ToLower(modelProvider)]
	if !ok || envVar == "" {
		return nil
	}
	if os.Getenv(envVar) == "" {
		return fmt.Errorf("required API key %s not set for model provider %s", envVar, modelProvider)
	}
	return nil
}

// buildRegistryResolvedServers builds Docker images for MCP servers that were resolved from the registry.
// This is similar to buildMCPServers, but for registry-resolved servers at runtime.
func buildRegistryResolvedServers(tempDir string, manifest *common.AgentManifest, verbose bool) error {
	if manifest == nil {
		return nil
	}

	for _, srv := range manifest.McpServers {
		// Only build command-type servers that came from registry resolution (have a registry build path)
		if srv.Type != "command" || !strings.HasPrefix(srv.Build, "registry/") {
			continue
		}

		// Server directory is at tempDir/registry/<name>
		serverDir := filepath.Join(tempDir, srv.Build)
		if _, err := os.Stat(serverDir); err != nil {
			return fmt.Errorf("registry server directory not found for %s: %w", srv.Name, err)
		}

		dockerfilePath := filepath.Join(serverDir, "Dockerfile")
		if _, err := os.Stat(dockerfilePath); err != nil {
			return fmt.Errorf("dockerfile not found for registry server %s (%s): %w", srv.Name, dockerfilePath, err)
		}

		imageName := project.ConstructMCPServerImageName(manifest.Name, srv.Name)
		if verbose {
			fmt.Printf("Building registry-resolved MCP server %s -> %s\n", srv.Name, imageName)
		}

		exec := docker.NewExecutor(verbose, serverDir)
		if err := exec.Build(imageName, "."); err != nil {
			return fmt.Errorf("docker build failed for registry server %s: %w", srv.Name, err)
		}
	}

	return nil
}

// writeResolvedMCPServerConfig writes resolved MCP server configuration to a JSON file that matches the agent's framework's MCP format.
// This enables registry-run agents to use registry-typed MCP servers at runtime.
// Similar to writeResolvedMCPServerConfig in runtime/agentregistry_runtime.go
// TODO: If we add support for more agent languages/frameworks, expand this to work with those formats.
func writeResolvedMCPServerConfig(tempDir string, manifest *common.AgentManifest, version string, verbose bool) error {
	if manifest == nil || len(manifest.McpServers) == 0 {
		return nil
	}

	// Convert resolved servers to the agent's framework's MCP format
	var mcpServers []common.PythonMCPServer

	for _, srv := range manifest.McpServers {
		// Only include resolved servers for the agent
		if srv.Type == "registry" {
			continue
		}

		pythonServer := common.PythonMCPServer{
			Name: srv.Name,
			Type: srv.Type,
		}

		if srv.Type == "remote" {
			pythonServer.URL = srv.URL
			if len(srv.Headers) > 0 {
				pythonServer.Headers = srv.Headers
			}
		}
		// For command type, the Python code constructs URL as f"http://{server_name}:3000/mcp"
		// So we don't need to include url in the dict

		mcpServers = append(mcpServers, pythonServer)
	}

	if len(mcpServers) == 0 {
		return nil // No resolved servers to write
	}

	// Determine config directory path based on whether version is provided
	var configDir string
	if version != "" {
		// Registry runs: use version-specific path {agentName}/{version}/
		sanitizedVersion := utils.SanitizeVersion(version)
		configDir = filepath.Join(tempDir, manifest.Name, sanitizedVersion)
	} else {
		// Local runs: use simple path {agentName}/
		configDir = filepath.Join(tempDir, manifest.Name)
	}

	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent config directory: %w", err)
	}

	// Write to JSON file at {agentName}/{version}/mcp-servers.json or {agentName}/mcp-servers.json
	// The agent container will mount this directory to /config, so the file will be at /config/mcp-servers.json
	configPath := filepath.Join(configDir, "mcp-servers.json")
	configData, err := json.MarshalIndent(mcpServers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP server config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write MCP server config file: %w", err)
	}

	if verbose {
		fmt.Printf("Wrote MCP server config for agent %s version %s to %s\n", manifest.Name, version, configPath)
	}

	return nil
}

// cleanupResolvedMCPServerConfig cleans up the resolved MCP server config directory.
// Used in the case that no registry-type MCP servers are present to ensure there is no previously-existing config.
func cleanupResolvedMCPServerConfig(tempDir string, agentName string, version string, verbose bool) error {
	var configDir string
	if version != "" {
		sanitizedVersion := utils.SanitizeVersion(version)
		configDir = filepath.Join(tempDir, agentName, sanitizedVersion)
	} else {
		configDir = filepath.Join(tempDir, agentName)
	}

	configPath := filepath.Join(configDir, "mcp-servers.json")
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove MCP server config file: %w", err)
	}

	if verbose {
		fmt.Printf("Cleaned up resolved MCP server config for agent %s version %s\n", agentName, version)
	}

	return nil
}

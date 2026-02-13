package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
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

var buildFlag bool

func init() {
	RunCmd.Flags().BoolVar(&buildFlag, "build", true, "Build the agent and MCP servers before running")
}

var providerAPIKeys = map[string]string{
	"openai":      "OPENAI_API_KEY",
	"anthropic":   "ANTHROPIC_API_KEY",
	"azureopenai": "AZUREOPENAI_API_KEY",
	"gemini":      "GOOGLE_API_KEY",
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

	// Always clear previously resolved registry artifacts to avoid stale folders.
	if err := project.CleanupRegistryDir(projectDir, verbose); err != nil {
		return fmt.Errorf("failed to clean registry directory: %w", err)
	}

	var serversForConfig []common.PythonMCPServer

	// Resolve registry-type MCP servers if present
	if hasRegistryServers(manifest) {
		if verbose {
			fmt.Println("[registry-resolve] Detected registry-type MCP servers in manifest")
			fmt.Printf("[registry-resolve] Total MCP servers in manifest: %d\n", len(manifest.McpServers))
			for i, srv := range manifest.McpServers {
				fmt.Printf("[registry-resolve]   [%d] name=%q type=%q registryServerName=%q registryURL=%q\n",
					i, srv.Name, srv.Type, srv.RegistryServerName, srv.RegistryURL)
			}
		}

		if verbose {
			fmt.Println("[registry-resolve] Starting resolution of registry servers...")
		}
		servers, err := agentutils.ParseAgentManifestServers(manifest, verbose)
		if err != nil {
			return fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
		}
		manifest.McpServers = servers

		if verbose {
			fmt.Printf("[registry-resolve] Resolution complete. Total servers after resolution: %d\n", len(manifest.McpServers))
			for i, srv := range manifest.McpServers {
				fmt.Printf("[registry-resolve]   [%d] name=%q type=%q build=%q image=%q command=%q\n",
					i, srv.Name, srv.Type, srv.Build, srv.Image, srv.Command)
			}
		}

		var registryResolvedServers []models.McpServerType
		for _, srv := range manifest.McpServers {
			if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
				registryResolvedServers = append(registryResolvedServers, srv)
				if verbose {
					fmt.Printf("[registry-resolve] Including server %q for build (type=command, build=%q)\n", srv.Name, srv.Build)
				}
			} else if verbose {
				if srv.Type == "command" && srv.Build == "" && srv.Image != "" {
					fmt.Printf("[registry-resolve] Skipping server %q for build (OCI image %q ready to use)\n", srv.Name, srv.Image)
				} else {
					fmt.Printf("[registry-resolve] Skipping server %q for build (type=%q, build=%q)\n", srv.Name, srv.Type, srv.Build)
				}
			}
		}

		if len(registryResolvedServers) > 0 {
			if verbose {
				fmt.Printf("[registry-resolve] %d registry-resolved servers require directory setup\n", len(registryResolvedServers))
			}
			tmpManifest := *manifest
			tmpManifest.McpServers = registryResolvedServers
			// create directories and build images for the registry-resolved servers
			if err := project.EnsureMcpServerDirectories(projectDir, &tmpManifest, verbose); err != nil {
				return fmt.Errorf("failed to create MCP server directories: %w", err)
			}
		} else if verbose {
			fmt.Println("[registry-resolve] No registry-resolved command servers to build")
		}

		// Create MCP config for ALL resolved command-type servers (including OCI which don't need building)
		serversForConfig = common.PythonServersFromManifest(manifest)
		if verbose {
			fmt.Printf("[registry-resolve] Created %d server configurations for MCP config (includes OCI servers)\n", len(serversForConfig))
		}
	} else if verbose {
		fmt.Println("[registry-resolve] No registry-type MCP servers found in manifest")
	}

	// Always clean before run; only write config when we have resolved registry servers to persist.
	if err := common.RefreshMCPConfig(
		&common.MCPConfigTarget{BaseDir: projectDir, AgentName: manifest.Name},
		serversForConfig,
		verbose,
	); err != nil {
		return fmt.Errorf("failed to refresh resolved MCP server config: %w", err)
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
func hasRegistryServers(manifest *models.AgentManifest) bool {
	for _, srv := range manifest.McpServers {
		if srv.Type == "registry" {
			return true
		}
	}
	return false
}

// runFromManifest runs an agent based on a manifest, with optional pre-resolved data.
//   - overrides is non-nil when coming from runFromDirectory: compose and resolved MCP config
//     are already prepared (including cleanup), so this function skips resolution/cleanup.
//   - when overrides is nil, this function resolves registry MCP servers (if any), builds them,
//     renders compose, and creates mcp-servers.json for registry runs.
func runFromManifest(ctx context.Context, manifest *models.AgentManifest, version string, overrides *runContext) error {
	if manifest == nil {
		return fmt.Errorf("agent manifest is required")
	}

	var composeData []byte
	workDir := ""

	useOverrides := overrides != nil
	var serversForConfig []common.PythonMCPServer

	if useOverrides {
		// servers already resolved, compose already generated (i.e. from runFromDirectory)
		if verbose {
			fmt.Println("[registry-resolve] Using pre-resolved overrides from runFromDirectory")
		}
		composeData = overrides.composeData
		workDir = overrides.workDir
	} else {
		// Resolve registry-type MCP servers (if any) and build registry-resolved command servers.
		if hasRegistryServers(manifest) {
			if verbose {
				fmt.Println("[registry-resolve] Detected registry-type MCP servers in manifest (runFromManifest path)")
				fmt.Printf("[registry-resolve] Total MCP servers in manifest: %d\n", len(manifest.McpServers))
				for i, srv := range manifest.McpServers {
					fmt.Printf("[registry-resolve]   [%d] name=%q type=%q registryServerName=%q registryURL=%q version=%q\n",
						i, srv.Name, srv.Type, srv.RegistryServerName, srv.RegistryURL, srv.RegistryServerVersion)
				}
			}

			if verbose {
				fmt.Println("[registry-resolve] Starting resolution of registry servers...")
			}
			servers, err := agentutils.ParseAgentManifestServers(manifest, verbose)
			if err != nil {
				return fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
			}
			manifest.McpServers = servers

			if verbose {
				fmt.Printf("[registry-resolve] Resolution complete. Total servers after resolution: %d\n", len(manifest.McpServers))
				for i, srv := range manifest.McpServers {
					fmt.Printf("[registry-resolve]   [%d] name=%q type=%q build=%q image=%q command=%q\n",
						i, srv.Name, srv.Type, srv.Build, srv.Image, srv.Command)
				}
			}

			// Separate servers that need building (npm/pypi) from those that don't (OCI)
			var serversToBuild []models.McpServerType
			for _, srv := range manifest.McpServers {
				if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
					serversToBuild = append(serversToBuild, srv)
					if verbose {
						fmt.Printf("[registry-resolve] Including server %q for build (type=command, build=%q)\n", srv.Name, srv.Build)
					}
				} else if verbose {
					if srv.Type == "command" && srv.Build == "" && srv.Image != "" {
						fmt.Printf("[registry-resolve] Skipping server %q for build (OCI image %q ready to use)\n", srv.Name, srv.Image)
					} else {
						fmt.Printf("[registry-resolve] Skipping server %q for build (type=%q, build=%q)\n", srv.Name, srv.Type, srv.Build)
					}
				}
			}

			// Always create temp directory for mcp-servers.json (needed for both OCI and non-OCI servers)
			tmpDir, err := os.MkdirTemp("", "arctl-registry-resolve-*")
			if err != nil {
				return fmt.Errorf("failed to create temporary directory: %w", err)
			}
			if verbose {
				fmt.Printf("[registry-resolve] Created temporary directory: %s\n", tmpDir)
			}
			workDir = tmpDir

			// Build only servers that need building (npm/pypi, not OCI)
			if len(serversToBuild) > 0 {
				if verbose {
					fmt.Printf("[registry-resolve] %d registry-resolved servers require directory setup and build\n", len(serversToBuild))
				}

				tmpManifest := *manifest
				tmpManifest.McpServers = serversToBuild

				if verbose {
					fmt.Println("[registry-resolve] Creating MCP server directories...")
				}
				if err := project.EnsureMcpServerDirectories(tmpDir, &tmpManifest, verbose); err != nil {
					return fmt.Errorf("failed to create mcp server directories: %w", err)
				}

				if verbose {
					fmt.Println("[registry-resolve] Building registry-resolved server images...")
				}
				if err := buildRegistryResolvedServers(tmpDir, &tmpManifest, verbose); err != nil {
					return fmt.Errorf("failed to build registry server images: %w", err)
				}
			} else if verbose {
				fmt.Println("[registry-resolve] No registry-resolved command servers to build (OCI images only)")
			}

			// Create MCP config for ALL resolved command-type servers (including OCI which don't need building)
			serversForConfig = common.PythonServersFromManifest(manifest)
			if verbose {
				fmt.Printf("[registry-resolve] Created %d server configurations for MCP config (includes OCI servers)\n", len(serversForConfig))
			}
		} else if verbose {
			fmt.Println("[registry-resolve] No registry-type MCP servers found in manifest")
		}

		data, err := renderComposeFromManifest(manifest, version)
		if err != nil {
			return err
		}
		composeData = data

		// Clean and write the resolved MCP server config when this function performed resolution.
		if err := common.RefreshMCPConfig(
			&common.MCPConfigTarget{BaseDir: workDir, AgentName: manifest.Name, Version: version},
			serversForConfig,
			verbose,
		); err != nil {
			return err
		}
	}

	err := runAgent(ctx, composeData, manifest, workDir, buildFlag)

	// Clean up temp directory for registry-run agents
	if !useOverrides && workDir != "" && strings.Contains(workDir, "arctl-registry-resolve-") {
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

func renderComposeFromManifest(manifest *models.AgentManifest, version string) ([]byte, error) {
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	image := project.ConstructImageName("", manifest.Image, manifest.Name)

	// Sanitize version for filesystem use in template
	sanitizedVersion := utils.SanitizeVersion(version)

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name          string
		Version       string
		Image         string
		ModelProvider string
		ModelName     string
		EnvVars       []string
		McpServers    []models.McpServerType
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

func runAgent(ctx context.Context, composeData []byte, manifest *models.AgentManifest, workDir string, shouldBuild bool) error {
	if err := validateAPIKey(manifest.ModelProvider); err != nil {
		return err
	}

	composeCmd := docker.ComposeCommand()
	commonArgs := append(composeCmd[1:], "-f", "-")

	upArgs := []string{"up", "-d"}
	if shouldBuild {
		upArgs = append(upArgs, "--build")
	}
	upCmd := exec.CommandContext(ctx, composeCmd[0], append(commonArgs, upArgs...)...)
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
func buildRegistryResolvedServers(tempDir string, manifest *models.AgentManifest, verbose bool) error {
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

package agent

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
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
	cliUtils "github.com/agentregistry-dev/agentregistry/internal/cli/utils"
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
var envFlags []string

func init() {
	RunCmd.Flags().BoolVar(&buildFlag, "build", true, "Build the agent and MCP servers before running")
	RunCmd.Flags().StringArrayVarP(&envFlags, "env", "e", []string{}, "Environment variables to set when running the agent (KEY=VALUE)")
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

	envMap, err := cliUtils.ParseEnvFlags(envFlags)
	if err != nil {
		return err
	}

	target := args[0]
	if info, err := os.Stat(target); err == nil && info.IsDir() {
		fmt.Println("Running agent from local directory:", target)
		return runFromDirectory(cmd.Context(), target, envMap)
	}

	agentModel, err := apiClient.GetAgent(target)
	if err != nil {
		return fmt.Errorf("failed to resolve agent %q: %w", target, err)
	}
	manifest := agentModel.Agent.AgentManifest
	version := agentModel.Agent.Version
	return runFromManifest(cmd.Context(), &manifest, version, nil, envMap)
}

// runFromDirectory runs an agent from a local project directory. It resolves
// registry-type MCP servers at runtime, regenerating folders for servers that
// may have already been created during their initial add-cmd invocation. This
// redundancy is acceptable but could be optimized in the future.
func runFromDirectory(ctx context.Context, projectDir string, envMap map[string]string) error {
	manifest, err := project.LoadManifest(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load agent.yaml: %w", err)
	}

	resolvedSkills, err := resolveSkillsForRuntime(manifest)
	if err != nil {
		return fmt.Errorf("failed to resolve skills from agent manifest: %w", err)
	}
	if err := materializeSkillsForRuntime(
		resolvedSkills,
		skillsDirForAgentConfig(projectDir, manifest.Name, ""),
		verbose,
	); err != nil {
		return fmt.Errorf("failed to materialize skills: %w", err)
	}

	// Always clear previously resolved registry artifacts to avoid stale folders.
	if err := project.CleanupRegistryDir(projectDir, verbose); err != nil {
		return fmt.Errorf("failed to clean registry directory: %w", err)
	}

	registryResolvedServers, serversForConfig, err := resolveMCPServersForRuntime(manifest)
	if err != nil {
		return fmt.Errorf("failed to resolve MCP servers from agent manifest: %w", err)
	}
	if len(registryResolvedServers) > 0 {
		tmpManifest := *manifest
		tmpManifest.McpServers = registryResolvedServers
		// create directories and build images for the registry-resolved servers
		if err := project.EnsureMcpServerDirectories(projectDir, &tmpManifest, verbose); err != nil {
			return fmt.Errorf("failed to create MCP server directories: %w", err)
		}
	}

	// Always clean before run; only write config when we have resolved registry servers to persist.
	if err := common.RefreshMCPConfig(
		&common.MCPConfigTarget{BaseDir: projectDir, AgentName: manifest.Name},
		serversForConfig,
		verbose,
	); err != nil {
		return fmt.Errorf("failed to refresh resolved MCP server config: %w", err)
	}

	var promptsForConfig []common.PythonPrompt
	if hasManifestPrompts(manifest) {
		if verbose {
			fmt.Printf("[prompt-resolve] Detected %d prompts in manifest\n", len(manifest.Prompts))
		}
		resolved, err := agentutils.ResolveManifestPrompts(manifest, verbose)
		if err != nil {
			return fmt.Errorf("failed to resolve prompts: %w", err)
		}
		promptsForConfig = resolved
	}

	if err := common.RefreshPromptsConfig(
		&common.MCPConfigTarget{BaseDir: projectDir, AgentName: manifest.Name},
		promptsForConfig,
		verbose,
	); err != nil {
		return fmt.Errorf("failed to refresh prompts config: %w", err)
	}

	if err := project.RegeneratePromptsLoader(projectDir, manifest, verbose); err != nil {
		if verbose {
			fmt.Printf("[prompt-resolve] Warning: could not regenerate prompts_loader.py: %v\n", err)
		}
	}

	if err := project.EnsureOtelCollectorConfig(projectDir, manifest, verbose); err != nil {
		return err
	}

	if err := project.RegenerateDockerCompose(projectDir, manifest, "", verbose); err != nil {
		return fmt.Errorf("failed to refresh docker-compose.yaml: %w", err)
	}

	return runFromManifest(ctx, manifest, "", &runContext{
		workDir: projectDir,
	}, envMap)
}

func skillsDirForAgentConfig(baseDir, agentName, version string) string {
	configDir, _ := common.ComputeMCPConfigPath(&common.MCPConfigTarget{
		BaseDir:   baseDir,
		AgentName: agentName,
		Version:   version,
	})
	if configDir == "" {
		return ""
	}
	return filepath.Join(configDir, "skills")
}

// hasManifestPrompts checks if the manifest has any prompt references.
func hasManifestPrompts(manifest *models.AgentManifest) bool {
	return len(manifest.Prompts) > 0
}

// runFromManifest runs an agent based on a manifest, with optional pre-resolved data.
// When overrides is non-nil (from runFromDirectory), the working directory is already
// prepared. Otherwise, this function resolves all runtime dependencies first.
// EnvMap contains --env KEY=VALUE overrides (e.g. API keys) and is used for validation and compose process env.
func runFromManifest(ctx context.Context, manifest *models.AgentManifest, version string, overrides *runContext, envMap map[string]string) error {
	if manifest == nil {
		return fmt.Errorf("agent manifest is required")
	}

	hostPort, err := freePort()
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	var workDir string
	var cleanupWorkDir bool

	if overrides != nil {
		if verbose {
			fmt.Println("[registry-resolve] Using pre-resolved overrides from runFromDirectory")
		}
		workDir = overrides.workDir
	} else {
		workDir, cleanupWorkDir, err = resolveManifestDependencies(manifest, version)
		if err != nil {
			return err
		}
	}

	composeData, err := renderComposeFromManifest(manifest, version, hostPort)
	if err != nil {
		return err
	}

	err = runAgent(ctx, composeData, manifest, workDir, buildFlag, hostPort, envMap)

	if cleanupWorkDir && workDir != "" {
		if cleanupErr := os.RemoveAll(workDir); cleanupErr != nil {
			fmt.Fprintf(os.Stderr, "Warning: failed to remove temporary directory %s: %v\n", workDir, cleanupErr)
		}
	}

	return err
}

type runContext struct {
	workDir string
}

// resolveManifestDependencies resolves all runtime dependencies for a manifest-based
// run: registry MCP servers, skills, telemetry config, and prompts.
// Returns the working directory and whether it should be cleaned up after the run.
func resolveManifestDependencies(manifest *models.AgentManifest, version string) (string, bool, error) {
	var workDir string
	var cleanup bool
	var serversForConfig []common.PythonMCPServer

	if hasRegistryServers(manifest) {
		var err error
		workDir, serversForConfig, err = resolveAndBuildRegistryServers(manifest)
		if err != nil {
			return "", false, err
		}
		cleanup = true
	} else if verbose {
		fmt.Println("[registry-resolve] No registry-type MCP servers found in manifest")
	}

	resolvedSkills, err := resolveSkillsForRuntime(manifest)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve skills from agent manifest: %w", err)
	}
	if len(resolvedSkills) > 0 && workDir == "" {
		tmpDir, err := os.MkdirTemp("", "arctl-skill-resolve-*")
		if err != nil {
			return "", false, fmt.Errorf("failed to create temporary directory for skills: %w", err)
		}
		workDir = tmpDir
		cleanup = true
		if verbose {
			fmt.Printf("[skill-resolve] Created temporary directory: %s\n", tmpDir)
		}
	}
	if err := materializeSkillsForRuntime(
		resolvedSkills,
		skillsDirForAgentConfig(workDir, manifest.Name, version),
		verbose,
	); err != nil {
		return "", false, fmt.Errorf("failed to materialize skills: %w", err)
	}

	if err := project.EnsureOtelCollectorConfig(workDir, manifest, verbose); err != nil {
		return "", false, err
	}

	if err := common.RefreshMCPConfig(
		&common.MCPConfigTarget{BaseDir: workDir, AgentName: manifest.Name, Version: version},
		serversForConfig,
		verbose,
	); err != nil {
		return "", false, err
	}

	promptsForConfig, err := resolvePrompts(manifest)
	if err != nil {
		return "", false, err
	}
	if err := common.RefreshPromptsConfig(
		&common.MCPConfigTarget{BaseDir: workDir, AgentName: manifest.Name, Version: version},
		promptsForConfig,
		verbose,
	); err != nil {
		return "", false, err
	}

	return workDir, cleanup, nil
}

// resolveAndBuildRegistryServers resolves registry-type MCP servers from the manifest,
// builds Docker images for servers that need it, and returns a temp working directory
// along with server configurations for mcp-servers.json.
func resolveAndBuildRegistryServers(manifest *models.AgentManifest) (string, []common.PythonMCPServer, error) {
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
		return "", nil, fmt.Errorf("failed to parse agent manifest mcp servers: %w", err)
	}
	manifest.McpServers = servers

	if verbose {
		fmt.Printf("[registry-resolve] Resolution complete. Total servers after resolution: %d\n", len(manifest.McpServers))
		for i, srv := range manifest.McpServers {
			fmt.Printf("[registry-resolve]   [%d] name=%q type=%q build=%q image=%q command=%q\n",
				i, srv.Name, srv.Type, srv.Build, srv.Image, srv.Command)
		}
	}

	serversToBuild := filterServersToBuild(manifest.McpServers)

	tmpDir, err := os.MkdirTemp("", "arctl-registry-resolve-*")
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	if verbose {
		fmt.Printf("[registry-resolve] Created temporary directory: %s\n", tmpDir)
	}

	if len(serversToBuild) > 0 { //nolint:nestif
		if verbose {
			fmt.Printf("[registry-resolve] %d registry-resolved servers require directory setup and build\n", len(serversToBuild))
		}

		tmpManifest := *manifest
		tmpManifest.McpServers = serversToBuild

		if verbose {
			fmt.Println("[registry-resolve] Creating MCP server directories...")
		}
		if err := project.EnsureMcpServerDirectories(tmpDir, &tmpManifest, verbose); err != nil {
			return "", nil, fmt.Errorf("failed to create mcp server directories: %w", err)
		}

		if verbose {
			fmt.Println("[registry-resolve] Building registry-resolved server images...")
		}
		if err := buildRegistryResolvedServers(tmpDir, &tmpManifest, verbose); err != nil {
			return "", nil, fmt.Errorf("failed to build registry server images: %w", err)
		}
	} else if verbose {
		fmt.Println("[registry-resolve] No registry-resolved command servers to build (OCI images only)")
	}

	serversForConfig := common.PythonServersFromManifest(manifest)
	if verbose {
		fmt.Printf("[registry-resolve] Created %d server configurations for MCP config (includes OCI servers)\n", len(serversForConfig))
	}

	return tmpDir, serversForConfig, nil
}

// filterServersToBuild separates servers that need building (npm/pypi) from those
// that don't (OCI images).
func filterServersToBuild(servers []models.McpServerType) []models.McpServerType {
	var result []models.McpServerType
	for _, srv := range servers {
		if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
			result = append(result, srv)
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
	return result
}

// resolvePrompts resolves prompt references from the manifest into configurations.
func resolvePrompts(manifest *models.AgentManifest) ([]common.PythonPrompt, error) {
	if !hasManifestPrompts(manifest) {
		return nil, nil
	}
	if verbose {
		fmt.Printf("[prompt-resolve] Detected %d prompts in manifest\n", len(manifest.Prompts))
	}
	resolved, err := agentutils.ResolveManifestPrompts(manifest, verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve prompts: %w", err)
	}
	return resolved, nil
}

// freePort asks the OS for an available TCP port.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func renderComposeFromManifest(manifest *models.AgentManifest, version string, hostPort int) ([]byte, error) {
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	image := project.ConstructImageName("", manifest.Image, manifest.Name)

	// Sanitize version for filesystem use in template
	sanitizedVersion := utils.SanitizeVersion(version)

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name              string
		Version           string
		Image             string
		Port              int
		ModelProvider     string
		ModelName         string
		TelemetryEndpoint string
		HasSkills         bool
		EnvVars           []string
		McpServers        []models.McpServerType
	}{
		Name:              manifest.Name,
		Version:           sanitizedVersion,
		Image:             image,
		Port:              hostPort,
		ModelProvider:     manifest.ModelProvider,
		ModelName:         manifest.ModelName,
		TelemetryEndpoint: manifest.TelemetryEndpoint,
		HasSkills:         len(manifest.Skills) > 0,
		EnvVars:           project.EnvVarsFromManifest(manifest),
		McpServers:        manifest.McpServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render docker-compose template: %w", err)
	}
	return []byte(rendered), nil
}

func runAgent(ctx context.Context, composeData []byte, manifest *models.AgentManifest, workDir string, shouldBuild bool, hostPort int, envMap map[string]string) error {
	if err := validateAPIKey(manifest.ModelProvider, envMap); err != nil {
		return err
	}

	composeCmd := docker.ComposeCommand()
	commonArgs := append(composeCmd[1:], "-f", "-")

	// Env for compose subprocess so ${VAR} in the template resolve from --env and OS env
	// --env flag env vars take precedence over OS env vars (last duplicated key wins)
	baseEnv := os.Environ()
	for k, v := range envMap {
		baseEnv = append(baseEnv, k+"="+v)
	}

	upArgs := []string{"up", "-d"}
	if shouldBuild {
		upArgs = append(upArgs, "--build")
	}
	upCmd := exec.CommandContext(ctx, composeCmd[0], append(commonArgs, upArgs...)...)
	upCmd.Dir = workDir
	upCmd.Stdin = bytes.NewReader(composeData)
	upCmd.Env = baseEnv
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

	agentURL := fmt.Sprintf("http://localhost:%d", hostPort)
	if err := waitForAgent(ctx, agentURL, 60*time.Second); err != nil {
		printComposeLogs(composeCmd, commonArgs, composeData, workDir)
		return err
	}

	fmt.Printf("✓ Agent '%s' is running at %s\n", manifest.Name, agentURL)

	if err := launchChat(ctx, manifest.Name, agentURL); err != nil {
		return err
	}

	fmt.Println("\nStopping docker compose...")
	downCmd := exec.Command(composeCmd[0], append(commonArgs, "down")...)
	downCmd.Dir = workDir
	downCmd.Stdin = bytes.NewReader(composeData)
	downCmd.Env = baseEnv
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

func launchChat(ctx context.Context, agentName string, agentURL string) error {
	sessionID := protocol.GenerateContextID()
	client, err := a2aclient.NewA2AClient(agentURL, a2aclient.WithTimeout(60*time.Second))
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

func validateAPIKey(modelProvider string, extraEnv map[string]string) error {
	envVar, ok := providerAPIKeys[strings.ToLower(modelProvider)]
	if !ok || envVar == "" {
		return nil
	}
	// Check extra env map first (e.g. from --env flags)
	if v, exists := extraEnv[envVar]; exists && v != "" {
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

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
	agentmanifest "github.com/agentregistry-dev/agentregistry/internal/cli/agent/manifest"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
	agentutils "github.com/agentregistry-dev/agentregistry/internal/cli/agent/utils"
	"github.com/agentregistry-dev/agentregistry/internal/cli/common/docker"
	cliUtils "github.com/agentregistry-dev/agentregistry/internal/cli/utils"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/utils"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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

	agentModel, err := client.GetTyped(
		cmd.Context(),
		apiClient,
		v1alpha1.KindAgent,
		v1alpha1.DefaultNamespace,
		target,
		"",
		func() *v1alpha1.Agent { return &v1alpha1.Agent{} },
	)
	if err != nil {
		return fmt.Errorf("failed to resolve agent %q: %w", target, err)
	}
	resolved, err := agentmanifest.Resolve(cmd.Context(), apiClient, agentModel)
	if err != nil {
		return fmt.Errorf("failed to resolve agent runtime manifest: %w", err)
	}
	version := agentModel.Metadata.Version
	return runFromManifest(cmd.Context(), resolved, version, nil, envMap)
}

// runFromDirectory runs an agent from a local project directory. The on-disk
// agent.yaml is decoded as a v1alpha1.Agent envelope and immediately resolved
// against the registry (manifest.Resolve) so every MCP server entry arrives
// in terminal form (Type="command" or Type="remote") before any project
// regeneration happens.
func runFromDirectory(ctx context.Context, projectDir string, envMap map[string]string) error {
	agent, err := project.LoadAgent(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load agent.yaml: %w", err)
	}
	resolved, err := agentmanifest.Resolve(ctx, apiClient, agent)
	if err != nil {
		return fmt.Errorf("failed to resolve agent runtime manifest: %w", err)
	}

	resolvedSkills, err := resolveSkillsForRuntime(agent.Spec.Skills)
	if err != nil {
		return fmt.Errorf("failed to resolve skills from agent manifest: %w", err)
	}
	if err := materializeSkillsForRuntime(
		resolvedSkills,
		skillsDirForAgentConfig(projectDir, agent.Metadata.Name, ""),
		verbose,
	); err != nil {
		return fmt.Errorf("failed to materialize skills: %w", err)
	}

	// Always clear previously resolved registry artifacts to avoid stale folders.
	if err := project.CleanupRegistryDir(projectDir, verbose); err != nil {
		return fmt.Errorf("failed to clean registry directory: %w", err)
	}

	// Build directories + Dockerfiles for command-type servers that came
	// from npm/PyPI packages (Build="registry/<name>"). OCI images flow
	// straight through to docker-compose without local build setup.
	if hasBuildableServers(resolved.MCPServers) {
		if err := project.EnsureMcpServerDirectories(projectDir, resolved.MCPServers, verbose); err != nil {
			return fmt.Errorf("failed to create MCP server directories: %w", err)
		}
	}
	serversForConfig := pythonServersFromResolved(resolved.MCPServers)

	// Always clean before run; only write config when we have resolved registry servers to persist.
	if err := common.RefreshMCPConfig(
		&common.MCPConfigTarget{BaseDir: projectDir, AgentName: agent.Metadata.Name},
		serversForConfig,
		verbose,
	); err != nil {
		return fmt.Errorf("failed to refresh resolved MCP server config: %w", err)
	}

	var promptsForConfig []common.PythonPrompt
	if len(agent.Spec.Prompts) > 0 {
		if verbose {
			fmt.Printf("[prompt-resolve] Detected %d prompts in manifest\n", len(agent.Spec.Prompts))
		}
		resolvedPrompts, err := agentutils.ResolvePromptRefs(agent.Spec.Prompts, verbose)
		if err != nil {
			return fmt.Errorf("failed to resolve prompts: %w", err)
		}
		promptsForConfig = resolvedPrompts
	}

	if err := common.RefreshPromptsConfig(
		&common.MCPConfigTarget{BaseDir: projectDir, AgentName: agent.Metadata.Name},
		promptsForConfig,
		verbose,
	); err != nil {
		return fmt.Errorf("failed to refresh prompts config: %w", err)
	}

	if err := project.RegeneratePromptsLoader(projectDir, resolved, verbose); err != nil {
		if verbose {
			fmt.Printf("[prompt-resolve] Warning: could not regenerate prompts_loader.py: %v\n", err)
		}
	}

	if err := project.EnsureOtelCollectorConfig(projectDir, agent, verbose); err != nil {
		return err
	}

	if err := project.RegenerateDockerCompose(projectDir, resolved, "", verbose); err != nil {
		return fmt.Errorf("failed to refresh docker-compose.yaml: %w", err)
	}

	return runFromManifest(ctx, resolved, "", &runContext{
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

// runFromManifest runs an agent based on a fully-resolved manifest, with
// optional pre-resolved data. When overrides is non-nil (from
// runFromDirectory), the working directory is already prepared. Otherwise
// this function builds a temp work dir, lays out any buildable MCP server
// directories, and stages the prompts / mcp config the runtime needs.
// EnvMap contains --env KEY=VALUE overrides (e.g. API keys) and is used
// for validation and compose process env.
func runFromManifest(ctx context.Context, resolved *agentmanifest.ResolvedAgent, version string, overrides *runContext, envMap map[string]string) error {
	if resolved == nil || resolved.Agent == nil {
		return fmt.Errorf("resolved agent is required")
	}

	hostPort, err := freePort()
	if err != nil {
		return fmt.Errorf("failed to find available port: %w", err)
	}

	var workDir string
	var cleanupWorkDir bool

	if overrides != nil {
		workDir = overrides.workDir
	} else {
		workDir, cleanupWorkDir, err = stageManifestRuntime(ctx, resolved, version)
		if err != nil {
			return err
		}
	}

	composeData, err := renderComposeFromManifest(resolved, version, hostPort)
	if err != nil {
		return err
	}

	err = runAgent(ctx, composeData, resolved, workDir, buildFlag, hostPort, envMap)

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

// stageManifestRuntime sets up the temporary work directory the runtime
// needs: builds Docker images for command-type MCP servers shipped as
// npm/PyPI packages (Build="registry/..."), materializes skill artifacts,
// renders the otel collector config, and stages mcp-servers.json +
// prompts.json. The caller (runFromManifest) cleans up the returned
// workDir when cleanup==true.
//
// resolved is expected to carry only terminal-form MCP server entries
// (Type="command" or Type="remote"); manifest.Resolve guarantees this.
func stageManifestRuntime(_ context.Context, resolved *agentmanifest.ResolvedAgent, version string) (string, bool, error) {
	agent := resolved.Agent
	tmpDir, err := os.MkdirTemp("", "arctl-agent-run-*")
	if err != nil {
		return "", false, fmt.Errorf("failed to create temporary directory: %w", err)
	}
	cleanup := true
	workDir := tmpDir

	serversToBuild := filterServersToBuild(resolved.MCPServers)
	if len(serversToBuild) > 0 {
		if err := project.EnsureMcpServerDirectories(workDir, serversToBuild, verbose); err != nil {
			return "", false, fmt.Errorf("failed to create mcp server directories: %w", err)
		}
		if err := buildRegistryResolvedServers(workDir, agent.Metadata.Name, serversToBuild, verbose); err != nil {
			return "", false, fmt.Errorf("failed to build registry server images: %w", err)
		}
	}

	resolvedSkills, err := resolveSkillsForRuntime(agent.Spec.Skills)
	if err != nil {
		return "", false, fmt.Errorf("failed to resolve skills from agent manifest: %w", err)
	}
	if err := materializeSkillsForRuntime(
		resolvedSkills,
		skillsDirForAgentConfig(workDir, agent.Metadata.Name, version),
		verbose,
	); err != nil {
		return "", false, fmt.Errorf("failed to materialize skills: %w", err)
	}

	if err := project.EnsureOtelCollectorConfig(workDir, agent, verbose); err != nil {
		return "", false, err
	}

	if err := common.RefreshMCPConfig(
		&common.MCPConfigTarget{BaseDir: workDir, AgentName: agent.Metadata.Name, Version: version},
		pythonServersFromResolved(resolved.MCPServers),
		verbose,
	); err != nil {
		return "", false, err
	}

	promptsForConfig, err := resolvePrompts(agent.Spec.Prompts)
	if err != nil {
		return "", false, err
	}
	if err := common.RefreshPromptsConfig(
		&common.MCPConfigTarget{BaseDir: workDir, AgentName: agent.Metadata.Name, Version: version},
		promptsForConfig,
		verbose,
	); err != nil {
		return "", false, err
	}

	return workDir, cleanup, nil
}

// hasBuildableServers reports whether any resolved MCP server entry
// requires a local Docker build (npm/PyPI package translated to Build
// path "registry/<name>"). OCI images flow through to docker-compose
// without a build setup.
func hasBuildableServers(servers []agentmanifest.ResolvedMCPServer) bool {
	for _, srv := range servers {
		if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
			return true
		}
	}
	return false
}

// filterServersToBuild returns the subset of MCP server entries that need
// a local Docker build.
func filterServersToBuild(servers []agentmanifest.ResolvedMCPServer) []agentmanifest.ResolvedMCPServer {
	var result []agentmanifest.ResolvedMCPServer
	for _, srv := range servers {
		if srv.Type == "command" && strings.HasPrefix(srv.Build, "registry/") {
			result = append(result, srv)
		}
	}
	return result
}

// resolvePrompts resolves prompt ResourceRefs into the runtime's
// PythonPrompt list (basename keys + content fetched from the registry).
func resolvePrompts(prompts []v1alpha1.ResourceRef) ([]common.PythonPrompt, error) {
	if len(prompts) == 0 {
		return nil, nil
	}
	if verbose {
		fmt.Printf("[prompt-resolve] Detected %d prompts in manifest\n", len(prompts))
	}
	out, err := agentutils.ResolvePromptRefs(prompts, verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve prompts: %w", err)
	}
	return out, nil
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

func renderComposeFromManifest(resolved *agentmanifest.ResolvedAgent, version string, hostPort int) ([]byte, error) {
	agent := resolved.Agent
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	var specImage string
	if agent.Spec.Source != nil {
		specImage = agent.Spec.Source.Image
	}
	image := project.ConstructImageName("", specImage, agent.Metadata.Name)

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
		McpServers        []agentmanifest.ResolvedMCPServer
	}{
		Name:              agent.Metadata.Name,
		Version:           sanitizedVersion,
		Image:             image,
		Port:              hostPort,
		ModelProvider:     agent.Spec.ModelProvider,
		ModelName:         agent.Spec.ModelName,
		TelemetryEndpoint: agent.Spec.TelemetryEndpoint,
		HasSkills:         len(agent.Spec.Skills) > 0,
		EnvVars:           project.EnvVarsFromMCPServers(resolved.MCPServers),
		McpServers:        resolved.MCPServers,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to render docker-compose template: %w", err)
	}
	return []byte(rendered), nil
}

func runAgent(ctx context.Context, composeData []byte, resolved *agentmanifest.ResolvedAgent, workDir string, shouldBuild bool, hostPort int, envMap map[string]string) error {
	agent := resolved.Agent
	if err := validateAPIKey(agent.Spec.ModelProvider, envMap); err != nil {
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

	fmt.Printf("✓ Agent '%s' is running at %s\n", agent.Metadata.Name, agentURL)

	if err := launchChat(ctx, agent.Metadata.Name, agentURL); err != nil {
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

// buildRegistryResolvedServers builds Docker images for the subset of
// MCP servers that ship as npm/PyPI packages and need a local Dockerfile
// build before docker-compose can run them.
func buildRegistryResolvedServers(tempDir, agentName string, servers []agentmanifest.ResolvedMCPServer, verbose bool) error {
	for _, srv := range servers {
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

		imageName := project.ConstructMCPServerImageName(agentName, srv.Name)
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

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

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/docker"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/adk/python"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/project"
	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/tui"
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
	return runFromManifest(cmd.Context(), &manifest, nil)
}

func runFromDirectory(ctx context.Context, projectDir string) error {
	manifest, err := project.LoadManifest(projectDir)
	if err != nil {
		return fmt.Errorf("failed to load agent.yaml: %w", err)
	}

	if err := project.RegenerateDockerCompose(projectDir, manifest, verbose); err != nil {
		return fmt.Errorf("failed to refresh docker-compose.yaml: %w", err)
	}

	composePath := filepath.Join(projectDir, "docker-compose.yaml")
	data, err := os.ReadFile(composePath)
	if err != nil {
		return fmt.Errorf("failed to read docker-compose.yaml: %w", err)
	}

	return runFromManifest(ctx, manifest, &runContext{
		composeData: data,
		workDir:     projectDir,
	})
}

func runFromManifest(ctx context.Context, manifest *common.AgentManifest, overrides *runContext) error {
	if manifest == nil {
		return fmt.Errorf("agent manifest is required")
	}

	var composeData []byte
	workDir := ""

	if overrides != nil {
		composeData = overrides.composeData
		workDir = overrides.workDir
	}

	if composeData == nil {
		data, err := renderComposeFromManifest(manifest)
		if err != nil {
			return err
		}
		composeData = data
	}

	return runAgent(ctx, composeData, manifest, workDir)
}

type runContext struct {
	composeData []byte
	workDir     string
}

func renderComposeFromManifest(manifest *common.AgentManifest) ([]byte, error) {
	gen := python.NewPythonGenerator()
	templateBytes, err := gen.ReadTemplateFile("docker-compose.yaml.tmpl")
	if err != nil {
		return nil, fmt.Errorf("failed to read docker-compose template: %w", err)
	}

	image := manifest.Image
	if image == "" {
		image = project.ConstructImageName("", manifest.Name)
	}

	rendered, err := gen.RenderTemplate(string(templateBytes), struct {
		Name          string
		Image         string
		ModelProvider string
		ModelName     string
		EnvVars       []string
		McpServers    []common.McpServerType
	}{
		Name:          manifest.Name,
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

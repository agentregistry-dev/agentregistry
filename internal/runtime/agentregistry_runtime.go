package runtime

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"

	"go.yaml.in/yaml/v3"
)

//go:embed agentgateway.Dockerfile
var agentGatewayDockerfile string

type AgentRegistryRuntime interface {
	ReconcileMCPServers(
		ctx context.Context,
		desired []*registry.MCPServerRunRequest,
	) error
}

type agentRegistryRuntime struct {
	registryTranslator      registry.Translator
	dockerComposeTranslator dockercompose.Translator
	runtimeDir              string
	verbose                 bool
}

func NewAgentRegistryRuntime(
	registryTranslator registry.Translator,
	dockerComposeTranslator dockercompose.Translator,
	runtimeDir string,
	verbose bool,
) AgentRegistryRuntime {
	return &agentRegistryRuntime{
		registryTranslator:      registryTranslator,
		dockerComposeTranslator: dockerComposeTranslator,
		runtimeDir:              runtimeDir,
		verbose:                 verbose,
	}
}

func (r *agentRegistryRuntime) ReconcileMCPServers(
	ctx context.Context,
	requests []*registry.MCPServerRunRequest,
) error {
	desiredState := &api.DesiredState{}
	for _, req := range requests {
		mcpServer, err := r.registryTranslator.TranslateMCPServer(
			context.TODO(),
			req,
		)
		if err != nil {
			return fmt.Errorf("translate mcp server %s: %w", req.RegistryServer.Name, err)
		}
		desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
	}

	runtimeCfg, err := r.dockerComposeTranslator.TranslateRuntimeConfig(ctx, desiredState)
	if err != nil {
		return fmt.Errorf("translate runtime config: %w", err)
	}

	return r.ensureRuntime(ctx, runtimeCfg)
}

func (r *agentRegistryRuntime) ensureRuntime(
	ctx context.Context,
	cfg *dockercompose.AiRuntimeConfig,
) error {
	// step 0: ensure custom agent gateway image exists
	if err := r.ensureAgentGatewayImage(ctx); err != nil {
		return fmt.Errorf("failed to ensure agent gateway image: %w", err)
	}
	// step 1: ensure the root runtime dir exists
	if err := os.MkdirAll(r.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	// step 2: write the docker compose yaml to the dir
	dockerComposeYaml, err := cfg.DockerCompose.MarshalYAML()
	if err != nil {
		return fmt.Errorf("failed to marshal docker compose yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.runtimeDir, "docker-compose.yaml"), dockerComposeYaml, 0644); err != nil {
		return fmt.Errorf("failed to write docker compose yaml: %w", err)
	}
	// step 3: write the agentconfig yaml to the dir
	agentGatewayYaml, err := yaml.Marshal(cfg.AgentGateway)
	if err != nil {
		return fmt.Errorf("failed to marshal agent config yaml: %w", err)
	}
	if err := os.WriteFile(filepath.Join(r.runtimeDir, "agent-gateway.yaml"), agentGatewayYaml, 0644); err != nil {
		return fmt.Errorf("failed to write agent config yaml: %w", err)
	}
	// step 4: start docker compose with -d --remove-orphans --force-recreate
	// Using --force-recreate ensures all containers are recreated even if config hasn't changed
	cmd := exec.CommandContext(ctx, "docker", "compose", "up", "-d", "--remove-orphans", "--force-recreate")
	cmd.Dir = r.runtimeDir
	if r.verbose {
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	} else {
		cmd.Stdout = nil
		cmd.Stderr = nil
	}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to start docker compose: %w", err)
	}

	fmt.Println("✓ Docker containers started")

	return nil
}

// TODO: we'll probably want to publish this image, instead of doing it this way.
func (r *agentRegistryRuntime) ensureAgentGatewayImage(ctx context.Context) error {
	imageName := "arctl-agentgateway:latest"

	// Check if image already exists
	checkCmd := exec.CommandContext(ctx, "docker", "image", "inspect", imageName)
	if err := checkCmd.Run(); err == nil {
		// Image exists
		if r.verbose {
			fmt.Printf("Using existing agent gateway image: %s\n", imageName)
		}
		return nil
	}

	// Image doesn't exist, build it
	fmt.Printf("Building custom agent gateway image with npx/uvx support...\n")

	// Write Dockerfile to runtime directory
	dockerfilePath := filepath.Join(r.runtimeDir, "agentgateway.Dockerfile")
	if err := os.MkdirAll(r.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	if err := os.WriteFile(dockerfilePath, []byte(agentGatewayDockerfile), 0644); err != nil {
		return fmt.Errorf("failed to write Dockerfile: %w", err)
	}

	// Build the image
	buildCmd := exec.CommandContext(ctx, "docker", "build",
		"-f", dockerfilePath,
		"-t", imageName,
		r.runtimeDir)

	if r.verbose {
		buildCmd.Stdout = os.Stdout
		buildCmd.Stderr = os.Stderr
	}

	if err := buildCmd.Run(); err != nil {
		return fmt.Errorf("failed to build agent gateway image: %w", err)
	}

	fmt.Println("✓ Agent gateway image built successfully")
	return nil
}

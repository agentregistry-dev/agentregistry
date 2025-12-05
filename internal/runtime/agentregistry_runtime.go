package runtime

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	"github.com/agentregistry-dev/agentregistry/internal/utils"

	"go.yaml.in/yaml/v3"
)

type AgentRegistryRuntime interface {
	ReconcileAll(
		ctx context.Context,
		servers []*registry.MCPServerRunRequest,
		agents []*registry.AgentRunRequest,
	) error
}

type agentRegistryRuntime struct {
	registryTranslator registry.Translator
	runtimeTranslator  api.RuntimeTranslator
	runtimeDir         string
	verbose            bool
}

func NewAgentRegistryRuntime(
	registryTranslator registry.Translator,
	translator api.RuntimeTranslator,
	runtimeDir string,
	verbose bool,
) AgentRegistryRuntime {
	return &agentRegistryRuntime{
		registryTranslator: registryTranslator,
		runtimeTranslator:  translator,
		runtimeDir:         runtimeDir,
		verbose:            verbose,
	}
}

func (r *agentRegistryRuntime) ReconcileAll(
	ctx context.Context,
	serverRequests []*registry.MCPServerRunRequest,
	agentRequests []*registry.AgentRunRequest,
) error {
	desiredState := &api.DesiredState{}
	for _, req := range serverRequests {
		mcpServer, err := r.registryTranslator.TranslateMCPServer(context.TODO(), req)
		if err != nil {
			return fmt.Errorf("translate mcp server %s: %w", req.RegistryServer.Name, err)
		}
		desiredState.MCPServers = append(desiredState.MCPServers, mcpServer)
	}

	for _, req := range agentRequests {
		agent, err := r.registryTranslator.TranslateAgent(context.TODO(), req)
		if err != nil {
			return fmt.Errorf("translate agent %s: %w", req.RegistryAgent.Name, err)
		}
		desiredState.Agents = append(desiredState.Agents, agent)

		// Write registry-resolved MCP server config file for this agent
		// This is used to inject MCP servers resolved from a registry into the agent at runtime
		if len(req.ResolvedMCPServers) > 0 {
			if err := r.writeResolvedMCPServerConfig(req.RegistryAgent.Name, req.RegistryAgent.Version, req.ResolvedMCPServers); err != nil {
				// Log error but don't fail deployment
				fmt.Printf("Error: Failed to write MCP server config for agent %s: %v\n", req.RegistryAgent.Name, err)
			}
		} else {
			// No registry-type MCP servers, we clean up the resolved MCP server config in case any previous one exists.
			if err := r.cleanupResolvedMCPServerConfig(req.RegistryAgent.Name, req.RegistryAgent.Version); err != nil {
				return fmt.Errorf("failed to cleanup resolved MCP server config: %w", err)
			}
		}
	}

	runtimeCfg, err := r.runtimeTranslator.TranslateRuntimeConfig(ctx, desiredState)
	if err != nil {
		return fmt.Errorf("translate runtime config: %w", err)
	}

	if r.verbose {
		fmt.Printf("desired state: agents=%d MCP servers=%d\n", len(desiredState.Agents), len(desiredState.MCPServers))
	}

	return r.ensureRuntime(ctx, runtimeCfg)
}

func (r *agentRegistryRuntime) ensureRuntime(
	ctx context.Context,
	cfg *api.AIRuntimeConfig,
) error {

	switch cfg.Type {
	case api.RuntimeConfigTypeLocal:
		return r.ensureLocalRuntime(ctx, cfg.Local)
	// TODO: Add a handler for other runtimes
	default:
		return fmt.Errorf("unsupported runtime config type: %v", cfg.Type)
	}
}

func (r *agentRegistryRuntime) ensureLocalRuntime(
	ctx context.Context,
	cfg *api.LocalRuntimeConfig,
) error {
	// step 1: ensure the root runtime dir exists
	if err := os.MkdirAll(r.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}
	// step 2: write the docker compose yaml to the dir
	dockerComposeYaml, err := cfg.DockerCompose.MarshalYAML()
	if err != nil {
		return fmt.Errorf("failed to marshal docker compose yaml: %w", err)
	}
	if r.verbose {
		fmt.Printf("Docker Compose YAML:\n%s\n", string(dockerComposeYaml))
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
	if r.verbose {
		fmt.Printf("Agent Gateway YAML:\n%s\n", string(agentGatewayYaml))
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

	fmt.Println("Docker containers started")

	return nil
}

// writeResolvedMCPServerConfig writes resolved MCP server configuration to a JSON file that matches the agent's framework's MCP format.
// This enables registry-run agents to use registry-typed MCP servers at runtime.
// Similar to writeResolvedMCPServerConfig in cli/agent/run.go
// TODO: If we add support for more agent languages/frameworks, expand this to work with those formats.
func (r *agentRegistryRuntime) writeResolvedMCPServerConfig(agentName, version string, resolvedServers []*registry.MCPServerRunRequest) error {
	// Convert resolved servers to common.PythonMCPServer format
	var mcpServers []common.PythonMCPServer

	for _, serverReq := range resolvedServers {
		server := serverReq.RegistryServer
		// Skip servers with no remotes or packages
		if len(server.Remotes) == 0 && len(server.Packages) == 0 {
			continue
		}

		// Determine server type and build common.PythonMCPServer
		pythonServer := common.PythonMCPServer{
			Name: server.Name,
		}

		// use remote if prefer remote is true or there are no packages
		useRemote := len(server.Remotes) > 0 && (serverReq.PreferRemote || len(server.Packages) == 0)

		if useRemote {
			remote := server.Remotes[0]
			pythonServer.Type = "remote"
			pythonServer.URL = remote.URL

			// Process headers
			if len(remote.Headers) > 0 || len(serverReq.HeaderValues) > 0 {
				headers := make(map[string]string)
				// Add headers from server spec
				for _, h := range remote.Headers {
					headers[h.Name] = h.Value
				}
				// Override with header values from request
				for k, v := range serverReq.HeaderValues {
					headers[k] = v
				}
				if len(headers) > 0 {
					pythonServer.Headers = headers
				}
			}
		} else {
			// Command-based server
			pythonServer.Type = "command"
			// For command type, Python code constructs URL as f"http://{server_name}:3000/mcp"
			// So we don't need to include url in the dict
		}

		mcpServers = append(mcpServers, pythonServer)
	}

	// Determine config directory path based on whether version is provided
	// Runtime agents should always have a version, but handle empty gracefully
	var configDir string
	if version != "" {
		// Registry runs: use version-specific path {agentName}/{version}/
		sanitizedVersion := utils.SanitizeVersion(version)
		configDir = filepath.Join(r.runtimeDir, agentName, sanitizedVersion)
	} else {
		// Fallback for edge case (shouldn't happen for runtime agents)
		configDir = filepath.Join(r.runtimeDir, agentName)
	}
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return fmt.Errorf("failed to create agent config directory: %w", err)
	}

	// Write to JSON file at {agentName}/{version}/mcp-servers.json
	// The agent container will mount this directory to /config, so the file will be at /config/mcp-servers.json
	configPath := filepath.Join(configDir, "mcp-servers.json")
	configData, err := json.MarshalIndent(mcpServers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP server config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write MCP server config file: %w", err)
	}

	if r.verbose {
		fmt.Printf("Wrote MCP server config for agent %s version %s to %s\n", agentName, version, configPath)
	}

	return nil
}

// cleanupResolvedMCPServerConfig cleans up the resolved MCP server config directory.
// Used in the case that no registry-type MCP servers are present to ensure there is no previously-existing config.
func (r *agentRegistryRuntime) cleanupResolvedMCPServerConfig(agentName, version string) error {
	var configDir string
	if version != "" {
		sanitizedVersion := utils.SanitizeVersion(version)
		configDir = filepath.Join(r.runtimeDir, agentName, sanitizedVersion)
	} else {
		configDir = filepath.Join(r.runtimeDir, agentName)
	}

	configPath := filepath.Join(configDir, "mcp-servers.json")
	if err := os.Remove(configPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove MCP server config file: %w", err)
	}

	if r.verbose {
		fmt.Printf("Cleaned up resolved MCP server config for agent %s version %s\n", agentName, version)
	}

	return nil
}

package runtime

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/api"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"

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

		// Write resolved MCP server config file for this agent
		if len(req.ResolvedMCPServers) > 0 {
			if err := r.writeResolvedMCPServerConfig(req.RegistryAgent.Name, req.ResolvedMCPServers); err != nil {
				// Log warning but don't fail deployment
				if r.verbose {
					fmt.Printf("Warning: Failed to write MCP server config for agent %s: %v\n", req.RegistryAgent.Name, err)
				}
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

// writeResolvedMCPServerConfig writes resolved MCP server configuration to a faile
// The format matches what Python mcp_tools.py expects for loading MCP servers at runtime
func (r *agentRegistryRuntime) writeResolvedMCPServerConfig(agentName string, resolvedServers []*registry.MCPServerRunRequest) error {
	// Convert resolved servers to Python-compatible format
	var pythonServers []map[string]interface{}

	for _, serverReq := range resolvedServers {
		server := serverReq.RegistryServer

		// Determine server type and build Python dict
		serverDict := map[string]interface{}{
			"name": server.Name,
		}

		// Check if it's a remote server
		if len(server.Remotes) > 0 && (serverReq.PreferRemote || len(server.Packages) == 0) {
			remote := server.Remotes[0]
			serverDict["type"] = "remote"
			serverDict["url"] = remote.URL

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
					serverDict["headers"] = headers
				}
			}
		} else if len(server.Packages) > 0 {
			// Command-based server
			serverDict["type"] = "command"
			// For command type, Python code constructs URL as f"http://{server_name}:3000/mcp"
			// So we don't need to include url in the dict
		} else {
			// Skip servers with no packages or remotes
			continue
		}

		pythonServers = append(pythonServers, serverDict)
	}

	// Write to JSON file with agent-specific name
	// Each agent container mounts /config, so we use agent name to avoid conflicts
	configPath := filepath.Join(r.runtimeDir, fmt.Sprintf("mcp-servers-%s.json", agentName))
	configData, err := json.MarshalIndent(pythonServers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP server config: %w", err)
	}

	if err := os.WriteFile(configPath, configData, 0644); err != nil {
		return fmt.Errorf("failed to write MCP server config file: %w", err)
	}

	if r.verbose {
		fmt.Printf("Wrote MCP server config for agent %s to %s\n", agentName, configPath)
	}

	return nil
}

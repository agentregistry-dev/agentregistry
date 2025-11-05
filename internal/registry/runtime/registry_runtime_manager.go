package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type RuntimeManager interface {
	StartMCPServers() error
}

type runtimeManager struct {
	agentGatewayPort uint16
	startVerbose     bool
}

func NewRuntimeManager(agentGatewayPort uint16, startVerbose bool) RuntimeManager {
	return &runtimeManager{
		agentGatewayPort: agentGatewayPort,
		startVerbose:     startVerbose,
	}
}

func (m *runtimeManager) StartMCPServers() error {
	db, err := client.NewClientFromEnv()
	if err != nil {
		return fmt.Errorf("failed to create API client: %w", err)
	}

	// Get all installed servers
	servers, err := db.GetInstalledServers()
	if err != nil {
		return fmt.Errorf("failed to get installed servers: %w", err)
	}

	if len(servers) == 0 {
		fmt.Println("No MCP servers installed.")
		fmt.Println("Use 'arctl install mcp <server-name>' to install servers.")
		return nil
	}

	fmt.Printf("Found %d installed MCP server(s)\n", len(servers))

	// Get the runtime directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("failed to get home directory: %w", err)
	}
	runtimeDir := filepath.Join(homeDir, ".arctl", "runtime")

	// Convert installed servers to run requests
	var runRequests []*registry.MCPServerRunRequest
	for _, server := range servers {
		// Parse the server data
		var combinedData models.CombinedServerData
		if err := json.Unmarshal([]byte(server.Data), &combinedData); err != nil {
			log.Printf("Warning: Failed to parse server data for %s: %v", server.Name, err)
			continue
		}

		// Re-marshal to convert to apiv0.ServerJSON
		serverBytes, err := json.Marshal(combinedData.Server)
		if err != nil {
			log.Printf("Warning: Failed to marshal server data for %s: %v", server.Name, err)
			continue
		}

		var registryServer apiv0.ServerJSON
		if err := json.Unmarshal(serverBytes, &registryServer); err != nil {
			log.Printf("Warning: Failed to parse registry server for %s: %v", server.Name, err)
			continue
		}

		// Get installation config (env vars, args, headers)
		installation, err := db.GetInstallationByName("mcp", server.Name)
		if err != nil {
			log.Printf("Warning: Failed to get installation config for %s: %v", server.Name, err)
		}

		var config map[string]string
		if installation != nil && installation.Config != "" {
			if err := json.Unmarshal([]byte(installation.Config), &config); err != nil {
				log.Printf("Warning: Failed to parse installation config for %s: %v", server.Name, err)
			}
		}

		// Parse config into env, arg, and header values
		envValues := make(map[string]string)
		argValues := make(map[string]string)
		headerValues := make(map[string]string)

		for k, v := range config {
			// Simple heuristic: keys starting with "HEADER_" are headers
			// Keys starting with "ARG_" are arguments
			// Everything else is environment variables
			if len(k) > 7 && k[:7] == "HEADER_" {
				headerValues[k[7:]] = v
			} else if len(k) > 4 && k[:4] == "ARG_" {
				argValues[k[4:]] = v
			} else {
				envValues[k] = v
			}
		}

		runRequest := &registry.MCPServerRunRequest{
			RegistryServer: &registryServer,
			PreferRemote:   false, // Use stored preference or default to local
			EnvValues:      envValues,
			ArgValues:      argValues,
			HeaderValues:   headerValues,
		}

		runRequests = append(runRequests, runRequest)
		fmt.Printf("  - %s (v%s)\n", server.Name, server.Version)
	}

	if len(runRequests) == 0 {
		return fmt.Errorf("no valid servers to start")
	}

	// Create runtime with translators
	regTranslator := registry.NewTranslator()
	composeTranslator := dockercompose.NewAgentGatewayTranslator(runtimeDir, m.agentGatewayPort)
	agentRuntime := runtime.NewAgentRegistryRuntime(
		regTranslator,
		composeTranslator,
		runtimeDir,
		m.startVerbose,
	)

	if err := agentRuntime.ReconcileMCPServers(context.Background(), runRequests); err != nil {
		return fmt.Errorf("failed to start servers: %w", err)
	}

	fmt.Println("\n✓ All MCP servers started successfully")
	fmt.Printf("\nAgent Gateway endpoint: http://localhost:%d/mcp\n", m.agentGatewayPort)
	return nil
}

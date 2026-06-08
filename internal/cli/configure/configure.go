package configure

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/internal/cli/common"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

// clientConfigurers maps client names to their configurers
var clientConfigurers = map[string]ClientConfigurer{
	"vscode":      &VSCodeConfigurer{},
	"cursor":      &CursorConfigurer{},
	"claude-code": &ClaudeCodeConfigurer{},
}

func NewCommand(deps cliruntime.Deps) *cobra.Command {
	var configureURL, configurePort string

	cmd := &cobra.Command{
		Use:   cliruntime.CommandConfigure + " [client-name]",
		Short: "Configure a client",
		Long:  `Creates the .json configuration for each client, so it can connect to arctl.`,
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				out := cmd.OutOrStdout()
				fmt.Fprintln(out, "Supported clients:")
				for name, configurer := range clientConfigurers {
					fmt.Fprintf(out, "  %-15s - %s\n", name, configurer.GetClientName())
				}
				fmt.Fprintf(out, "\nUsage:\n  %s <client-name>\n", cmd.CommandPath())
				return nil
			}

			clientName := args[0]
			configurer, ok := clientConfigurers[clientName]
			if !ok {
				return fmt.Errorf("client %q is not supported; run 'arctl configure' to see supported clients", clientName)
			}

			url := fmt.Sprintf("http://localhost:%s/mcp", configurePort)
			if configureURL != "" {
				url = configureURL
			}

			configPath, err := configurer.GetConfigPath()
			if err != nil {
				return fmt.Errorf("failed to get config path: %v", err)
			}

			config, err := configurer.CreateConfig(url, configPath)
			if err != nil {
				return fmt.Errorf("failed to create %s config: %v", configurer.GetClientName(), err)
			}

			if err := writeConfigFile(configPath, config); err != nil {
				return fmt.Errorf("failed to write config file: %v", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Configured %s\n", configurer.GetClientName())
			return nil
		},
	}

	cmd.Flags().StringVar(&configureURL, "url", "", fmt.Sprintf("Custom MCP server URL (default: http://localhost:%s/mcp)", common.DefaultAgentGatewayPort))
	cmd.Flags().StringVar(&configurePort, "port", common.DefaultAgentGatewayPort, "Port for the MCP server")

	return cmd
}

func writeConfigFile(configPath string, config any) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Marshal config to JSON with pretty printing
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

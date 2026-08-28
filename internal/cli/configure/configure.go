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
	"kiro":        &KiroConfigurer{},
}

func NewCommand(_ cliruntime.Deps) *cobra.Command {
	cmd := &cobra.Command{
		Use:   cliruntime.CommandConfigure,
		Short: "Create the JSON configuration a client needs to connect to arctl",
		Long:  `Creates the .json configuration for each client, so it can connect to arctl.`,
	}

	for name, configurer := range clientConfigurers {
		cmd.AddCommand(newClientCommand(name, configurer))
	}

	return cmd
}

func newClientCommand(clientName string, configurer ClientConfigurer) *cobra.Command {
	var configureURL, configurePort, configureTokenEnv string

	cmd := &cobra.Command{
		Use:   clientName,
		Short: "Configure " + configurer.GetClientName(),
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			url := fmt.Sprintf("http://localhost:%s/mcp", configurePort)
			if configureURL != "" {
				if cmd.Flags().Changed("port") {
					fmt.Fprintln(cmd.OutOrStdout(), "Warning: --port is ignored when --url is set")
				}
				url = configureURL
			}

			configPath, err := configurer.GetConfigPath()
			if err != nil {
				return fmt.Errorf("failed to get config path: %v", err)
			}

			opts := CreateOptions{URL: url, TokenEnv: configureTokenEnv}
			config, err := configurer.CreateConfig(opts, configPath)
			if err != nil {
				return fmt.Errorf("failed to create %s config: %v", configurer.GetClientName(), err)
			}

			if err := writeConfigFile(configPath, config); err != nil {
				return fmt.Errorf("failed to write config file: %v", err)
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Configured %s\n", configurer.GetClientName())
			if configureTokenEnv != "" && clientName == "vscode" {
				// VSCode doesn't work off env vars and instead expects inputs, so setting token env is a special case for it
				fmt.Fprintf(cmd.OutOrStdout(), "Note: VS Code prompts for the token on first connection and stores it in its secret storage; the %s environment variable is not read\n", configureTokenEnv)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&configureURL, "url", "", fmt.Sprintf("Custom MCP server URL (default: http://localhost:%s/mcp)", common.DefaultAgentGatewayPort))
	cmd.Flags().StringVar(&configurePort, "port", common.DefaultAgentGatewayPort, "Port for the MCP server")
	cmd.Flags().StringVar(&configureTokenEnv, "token-env", "", "Name of the environment variable holding the MCP bearer token for static/direct access (e.g. ARCTL_MCP_TOKEN); written into the config as a reference the client expands at connect time. Clients that support OAuth can authenticate interactively instead, without this flag")

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

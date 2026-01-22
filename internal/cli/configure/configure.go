package configure

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	configureURL  string
	configurePort string
)

// clientConfigurers maps client names to their configurers
var clientConfigurers = map[string]ClientConfigurer{
	"vscode":      &VSCodeConfigurer{},
	"cursor":      &CursorConfigurer{},
	"claude-code": &ClaudeCodeConfigurer{},
}

// NewConfigureCmd creates the configure command
var ConfigureCmd = &cobra.Command{
	Use:   "configure [client-name]",
	Short: "Configure a client",
	Long:  `Creates the .json configuration for each client, so it can connect to arctl.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		// Show supported clients if no argument provided
		if len(args) == 0 {
			fmt.Println("Supported clients:")
			for name, configurer := range clientConfigurers {
				fmt.Printf("  %-15s - %s\n", name, configurer.GetClientName())
			}
			fmt.Println("\nUsage:")
			fmt.Println("  arctl configure <client-name>")
			fmt.Println("\nExamples:")
			fmt.Println("  arctl configure cursor")
			fmt.Println("  arctl configure claude-code --port 3000")
			fmt.Println("  arctl configure vscode --port 3000")
			return
		}

		clientName := args[0]

		// Get the configurer for the client
		configurer, ok := clientConfigurers[clientName]
		if !ok {
			log.Fatalf("Client '%s' is not supported. Run 'arctl configure' to see supported clients.", clientName)
		}

		// Build the URL
		url := fmt.Sprintf("http://localhost:%s/mcp", configurePort)
		if configureURL != "" {
			url = configureURL
		}

		// Get the config path
		configPath, err := configurer.GetConfigPath()
		if err != nil {
			log.Fatalf("Failed to get config path: %v", err)
		}

		// Create the config
		config, err := configurer.CreateConfig(url, configPath)
		if err != nil {
			log.Fatalf("Failed to create %s config: %v", configurer.GetClientName(), err)
		}

		// Write the config file
		if err := writeConfigFile(configPath, config); err != nil {
			log.Fatalf("Failed to write config file: %v", err)
		}

		fmt.Printf("✓ Configured %s\n", configurer.GetClientName())
	},
}

func init() {
	ConfigureCmd.Flags().StringVar(&configureURL, "url", "", "Custom MCP server URL (default: http://localhost:21212/mcp")
	ConfigureCmd.Flags().StringVar(&configurePort, "port", "21212", "Port for the MCP server")
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

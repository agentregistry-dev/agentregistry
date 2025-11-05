package configure

// ClientConfigurer defines the interface for client-specific MCP configuration
type ClientConfigurer interface {
	// GetConfigPath returns the path where the config file should be written
	GetConfigPath() (string, error)

	// CreateConfig creates or updates the MCP configuration for the client
	// It should read existing config, merge with the new server, and return the updated config
	CreateConfig(url string, configPath string) (interface{}, error)

	// GetClientName returns the display name of the client
	GetClientName() string
}

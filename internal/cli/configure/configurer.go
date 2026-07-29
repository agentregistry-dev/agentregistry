package configure

// CreateOptions carries the inputs for building a client's arctl MCP server entry.
type CreateOptions struct {
	// URL is the MCP server endpoint written into the client config.
	URL string
	// TokenEnv, when set, is the name of the environment variable holding the
	// bearer token. It is written into the config as a client-side reference
	// expanded by the client at connect time.
	TokenEnv string
}

// ClientConfigurer defines the interface for client-specific MCP configuration
type ClientConfigurer interface {
	// GetConfigPath returns the path where the config file should be written
	GetConfigPath() (string, error)

	// CreateConfig creates or updates the MCP configuration for the client.
	// It reads any existing config at configPath, replaces only the arctl
	// server entry, and returns the updated config with all other content
	// preserved to avoid breaking existing configurations.
	CreateConfig(opts CreateOptions, configPath string) (any, error)

	// GetClientName returns the display name of the client
	GetClientName() string
}

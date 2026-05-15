package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"

	env "github.com/caarlos0/env/v11"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress string `env:"SERVER_ADDRESS" envDefault:":8080"`
	MCPPort       uint16 `env:"MCP_PORT" envDefault:"0"`
	DatabaseURL   string `env:"DATABASE_URL" envDefault:"postgres://agentregistry:agentregistry@localhost:5432/agentregistry?sslmode=disable"`
	Version       string `env:"VERSION" envDefault:"dev"`
	JWTPrivateKey string `env:"JWT_PRIVATE_KEY" envDefault:""`
	LogLevel      string `env:"LOG_LEVEL" envDefault:"info"`

	// Platform mode: "docker" or "kubernetes". Controls which deployment
	// provider IDs are available in the UI. Defaults to "kubernetes" so
	// Helm/K8s deployments work without extra config; docker-compose.yml
	// explicitly sets this to "docker".
	PlatformMode string `env:"PLATFORM_MODE" envDefault:"kubernetes"`

	// Agent Gateway Configuration
	AgentGatewayPort uint16 `env:"AGENT_GATEWAY_PORT" envDefault:"8081"`

	// Runtime Configuration
	RuntimeDir string `env:"RUNTIME_DIR" envDefault:"/tmp/arctl-runtime"`
	Verbose    bool   `env:"VERBOSE" envDefault:"false"`
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	var cfg Config
	err := env.ParseWithOptions(&cfg, env.Options{
		Prefix: "AGENT_REGISTRY_",
	})
	if err != nil {
		slog.Error("failed to parse config", "error", err)
		os.Exit(1)
	}

	// Append a random suffix to RuntimeDir when the user has not set an
	// explicit override via the AGENT_REGISTRY_RUNTIME_DIR env var. This
	// prevents concurrent runs from sharing the same directory.
	if os.Getenv("AGENT_REGISTRY_RUNTIME_DIR") == "" {
		suffix, err := randomHex(8)
		if err != nil {
			slog.Error("failed to generate random runtime dir suffix", "error", err)
			os.Exit(1)
		}
		cfg.RuntimeDir = cfg.RuntimeDir + "-" + suffix
	}

	return &cfg
}

// randomHex returns a hex-encoded string of n random bytes.
func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

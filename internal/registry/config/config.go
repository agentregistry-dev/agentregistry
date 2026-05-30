package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"

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

	// SkipMigrations gates the server's Postgres migrator at startup.
	// Set true when migrations are applied out-of-band (e.g. by
	// `arctl db migrate up` from CI/CD ahead of the rollout).
	// Populated from AGENT_REGISTRY_SKIP_MIGRATIONS or the unprefixed
	// SKIP_MIGRATIONS as a fallback (prefixed wins; see NewConfig).
	// AppOptions.SkipMigrations wins over this env value when set
	// programmatically.
	SkipMigrations bool `env:"SKIP_MIGRATIONS" envDefault:"false"`
}

// NewConfig creates a new configuration with default values.
//
// Server-only entry point: NewConfig is called from registry.App() at
// server start; arctl does not call NewConfig, so the os.Exit(1)
// branches below (caarlos0/env parse failure and the SKIP_MIGRATIONS
// fallback parse) cannot fire during CLI invocations like `arctl db
// migrate`.
func NewConfig() *Config {
	var cfg Config
	err := env.ParseWithOptions(&cfg, env.Options{
		Prefix: "AGENT_REGISTRY_",
	})
	if err != nil {
		slog.Error("failed to parse config", "error", err)
		os.Exit(1)
	}

	// SkipMigrations also accepts the unprefixed SKIP_MIGRATIONS so
	// operators (and downstream binaries with a different env prefix)
	// can toggle the gate without the AGENT_REGISTRY_ prefix.
	// AGENT_REGISTRY_SKIP_MIGRATIONS wins when both are set.
	//
	// Symmetry: the prefixed env is parsed by caarlos0/env above, which
	// errors and triggers the os.Exit(1) on invalid bool values. The
	// bare-env path below mirrors that contract — an invalid value
	// fails NewConfig the same way rather than silently falling back to
	// false — so operators get the same loud feedback no matter which
	// name they used.
	if _, prefixed := os.LookupEnv("AGENT_REGISTRY_SKIP_MIGRATIONS"); !prefixed {
		if raw, ok := os.LookupEnv("SKIP_MIGRATIONS"); ok {
			parsed, perr := strconv.ParseBool(raw)
			if perr != nil {
				slog.Error("failed to parse SKIP_MIGRATIONS", "value", raw, "error", perr)
				os.Exit(1)
			}
			cfg.SkipMigrations = parsed
		}
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

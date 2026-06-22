package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"strconv"
	"time"

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

	// ControllerEventRetention is how long handled control-plane events remain
	// available for checkpoint replay. Set to 0 to disable event pruning.
	ControllerEventRetention time.Duration `env:"CONTROLLER_EVENT_RETENTION" envDefault:"24h"`
	// ControllerEventKeepAfterRevision preserves control-plane events newer than
	// this Postgres revision even when they are older than ControllerEventRetention.
	ControllerEventKeepAfterRevision int64 `env:"CONTROLLER_EVENT_KEEP_AFTER_REVISION" envDefault:"0"`
	// ControllerRetentionPruneBatchLimit caps rows removed per retention pass so
	// pruning cannot monopolize the database during startup or repair loops.
	ControllerRetentionPruneBatchLimit int `env:"CONTROLLER_RETENTION_PRUNE_BATCH_LIMIT" envDefault:"500"`

	// SkipMigrations gates the server's Postgres migrator at startup.
	// Set true when migrations are applied out-of-band (e.g. by
	// `arctl db migrate up` from CI/CD ahead of the rollout).
	// Populated from the unprefixed SKIP_MIGRATIONS env var (see
	// NewConfig) — deliberately prefix-free so the same name toggles
	// the gate across binaries regardless of their env prefix.
	// AppOptions.SkipMigrations wins over this env value when set
	// programmatically.
	SkipMigrations bool `env:"-"`
}

// NewConfig creates a new configuration with default values.
//
// Server-only entry point: NewConfig is called from registry.App() at
// server start; arctl does not call NewConfig, so the os.Exit(1)
// branches below (caarlos0/env parse failure and the SKIP_MIGRATIONS
// parse) cannot fire during CLI invocations like `arctl db
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

	// SkipMigrations reads the unprefixed SKIP_MIGRATIONS rather than the
	// AGENT_REGISTRY_ prefix the other fields use, so the same env var
	// toggles the gate across binaries regardless of their prefix. An
	// invalid value fails NewConfig loudly (mirroring caarlos0/env above)
	// rather than silently falling back to false.
	if raw, ok := os.LookupEnv("SKIP_MIGRATIONS"); ok {
		parsed, perr := strconv.ParseBool(raw)
		if perr != nil {
			slog.Error("failed to parse SKIP_MIGRATIONS", "value", raw, "error", perr)
			os.Exit(1)
		}
		cfg.SkipMigrations = parsed
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

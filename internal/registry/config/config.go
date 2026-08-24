package config

import (
	"log/slog"
	"os"
	"strconv"
	"time"

	env "github.com/caarlos0/env/v11"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string `env:"SERVER_ADDRESS" envDefault:":8080"`
	MCPPort                  uint16 `env:"MCP_PORT" envDefault:"0"`
	DatabaseURL              string `env:"DATABASE_URL" envDefault:"postgres://agentregistry:agentregistry@localhost:5432/agentregistry?sslmode=disable"`
	Version                  string `env:"VERSION" envDefault:"dev"`
	LogLevel                 string `env:"LOG_LEVEL" envDefault:"info"`
	SecretStore              string `env:"SECRET_STORE" envDefault:""`
	SecretStoreEncryptionKey string `env:"SECRET_STORE_ENCRYPTION_KEY" envDefault:""`
	SecretStoreNamespace     string `env:"SECRET_STORE_KUBERNETES_NAMESPACE" envDefault:"agentregistry-system"`

	// MCP Registry compatibility (read-only)
	//
	// MCPRegistryCompatEnabled toggles the read-only MCP Registry v0.1
	// compatibility API (GET /v0.1/servers ...), which re-exposes MCPServer
	// resources in the official server.json shape so registry-aware clients
	// (e.g. VS Code's MCP gallery) can discover them. The endpoint is
	// anonymous and flattens every namespace into one catalogue, and it
	// bypasses per-kind RBAC list filters, so it is OFF by default — enable
	// it only where an unauthenticated, cross-namespace MCP catalogue is
	// acceptable (a public OSS registry, or behind a trusted gateway).
	MCPRegistryCompatEnabled bool `env:"MCP_REGISTRY_COMPAT_ENABLED" envDefault:"false"`
	// MCPRegistryCompatPathPrefix optionally mounts the compatibility API
	// under a base prefix (e.g. "/mcp-registry"); empty serves the spec's
	// standard paths at the root. Clients append "/v0.1/servers" to the base
	// URL they're configured with, so any prefix set here must match that
	// configured base.
	MCPRegistryCompatPathPrefix string `env:"MCP_REGISTRY_COMPAT_PATH_PREFIX" envDefault:""`

	// Plugin marketplace compatibility (read-only)
	//
	// PluginMarketplaceCompatEnabled toggles the read-only Claude Code
	// marketplace.json compatibility API (GET /plugin-marketplace/marketplace.json),
	// which re-exposes resolved Plugin resources in the marketplace.json shape
	// so a bare URL to this endpoint can be registered directly with
	// `claude plugin marketplace add`. The endpoint flattens every namespace
	// into one catalogue and honors the same per-kind RBAC list filter as the
	// native Plugin read path (nil = public OSS behavior), so it is OFF by
	// default — enable it only where a Plugin catalogue at this scope is
	// acceptable (a public OSS registry, or behind a trusted gateway).
	PluginMarketplaceCompatEnabled bool `env:"PLUGIN_MARKETPLACE_COMPAT_ENABLED" envDefault:"false"`
	// PluginMarketplaceCompatPathPrefix optionally mounts the compatibility API
	// under a base prefix (e.g. "/plugins"); empty serves the standard
	// "/plugin-marketplace/marketplace.json" path at the root. Any prefix set
	// here must match the base URL registered with the consuming agent.
	PluginMarketplaceCompatPathPrefix string `env:"PLUGIN_MARKETPLACE_COMPAT_PATH_PREFIX" envDefault:""`

	// ControllerEventRetention is how long handled control-plane events remain
	// available for checkpoint replay. Set to 0 to disable event pruning.
	ControllerEventRetention time.Duration `env:"CONTROLLER_EVENT_RETENTION" envDefault:"24h"`
	// ControllerEventKeepAfterRevision preserves control-plane events newer than
	// this Postgres revision even when they are older than ControllerEventRetention.
	ControllerEventKeepAfterRevision int64 `env:"CONTROLLER_EVENT_KEEP_AFTER_REVISION" envDefault:"0"`
	// ControllerRetentionPruneBatchLimit caps rows removed per retention pass so
	// pruning cannot monopolize the database during startup or repair loops.
	ControllerRetentionPruneBatchLimit int `env:"CONTROLLER_RETENTION_PRUNE_BATCH_LIMIT" envDefault:"500"`
	// ControllerDiscoveryInterval is how often provider discovery snapshots are
	// materialized into discovered Deployment rows. Provider-specific cache
	// refreshes may have separate intervals.
	ControllerDiscoveryInterval time.Duration `env:"CONTROLLER_DISCOVERY_INTERVAL" envDefault:"60s"`
	// ControllerDiscoveryStaleAfterMisses is how many consecutive successful
	// discovery polls may omit a discovered Deployment before it is marked
	// not-ready/stale.
	ControllerDiscoveryStaleAfterMisses int `env:"CONTROLLER_DISCOVERY_STALE_AFTER_MISSES" envDefault:"3"`
	// ControllerDiscoveryDeleteAfterMisses is how many consecutive successful
	// discovery polls may omit a discovered Deployment before it is deleted.
	ControllerDiscoveryDeleteAfterMisses int `env:"CONTROLLER_DISCOVERY_DELETE_AFTER_MISSES" envDefault:"5"`

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

	return &cfg
}

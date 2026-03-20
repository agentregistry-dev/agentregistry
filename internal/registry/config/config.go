package config

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"

	env "github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string `env:"SERVER_ADDRESS" envDefault:":8080"`
	MCPPort                  uint16 `env:"MCP_PORT" envDefault:"0"`
	DatabaseURL              string `env:"DATABASE_URL" envDefault:"postgres://agentregistry:agentregistry@localhost:5432/agentregistry?sslmode=disable"`
	DatabaseVectorEnabled    bool   `env:"DATABASE_VECTOR_ENABLED" envDefault:"false"`
	SeedFrom                 string `env:"SEED_FROM" envDefault:""`
	EnrichServerData         bool   `env:"ENRICH_SERVER_DATA" envDefault:"false"`
	DisableBuiltinSeed       bool   `env:"DISABLE_BUILTIN_SEED" envDefault:"true"`
	Version                  string `env:"VERSION" envDefault:"dev"`
	GithubClientID           string `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret       string `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey            string `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth      bool   `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
	EnableRegistryValidation bool   `env:"ENABLE_REGISTRY_VALIDATION" envDefault:"true"`
	LogLevel                 string `env:"LOG_LEVEL" envDefault:"info"`

	// OIDC Configuration
	OIDCEnabled      bool   `env:"OIDC_ENABLED" envDefault:"false"`
	OIDCIssuer       string `env:"OIDC_ISSUER" envDefault:""`
	OIDCClientID     string `env:"OIDC_CLIENT_ID" envDefault:""`
	OIDCExtraClaims  string `env:"OIDC_EXTRA_CLAIMS" envDefault:""`
	OIDCEditPerms    string `env:"OIDC_EDIT_PERMISSIONS" envDefault:""`
	OIDCPublishPerms string `env:"OIDC_PUBLISH_PERMISSIONS" envDefault:""`
	OIDCReadPerms    string `env:"OIDC_READ_PERMISSIONS" envDefault:""`
	OIDCPushPerms    string `env:"OIDC_PUSH_PERMISSIONS" envDefault:""`
	OIDCDeletePerms  string `env:"OIDC_DELETE_PERMISSIONS" envDefault:""`
	OIDCDeployPerms  string `env:"OIDC_DEPLOY_PERMISSIONS" envDefault:""`

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

	// Embeddings / Semantic Search
	Embeddings EmbeddingsConfig
}

// EmbeddingsConfig captures configuration needed to generate embeddings
type EmbeddingsConfig struct {
	Enabled       bool   `env:"EMBEDDINGS_ENABLED" envDefault:"false"`
	Provider      string `env:"EMBEDDINGS_PROVIDER" envDefault:"openai"`
	Model         string `env:"EMBEDDINGS_MODEL" envDefault:"text-embedding-3-small"`
	Dimensions    int    `env:"EMBEDDINGS_DIMENSIONS" envDefault:"1536"`
	OpenAIAPIKey  string `env:"OPENAI_API_KEY" envDefault:""`
	OpenAIBaseURL string `env:"OPENAI_BASE_URL" envDefault:"https://api.openai.com/v1"`
	OpenAIOrg     string `env:"OPENAI_ORG" envDefault:""`
	OnPublish     bool   `env:"EMBEDDINGS_ON_PUBLISH" envDefault:"false"`
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		slog.Info("no .env file found or error loading .env file", "error", err)
	}
	var cfg Config
	err = env.ParseWithOptions(&cfg, env.Options{
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

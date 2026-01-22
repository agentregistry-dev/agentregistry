package config

import (
	"log"

	env "github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config holds the application configuration
// See .env.example for more documentation
type Config struct {
	ServerAddress            string `env:"SERVER_ADDRESS" envDefault:":8080"`
	MCPPort                  uint16 `env:"MCP_PORT" envDefault:"0"`
	DatabaseURL              string `env:"DATABASE_URL" envDefault:"postgres://agentregistry:agentregistry@localhost:5432/agent-registry?sslmode=disable"`
	SeedFrom                 string `env:"SEED_FROM" envDefault:""`
	EnrichServerData         bool   `env:"ENRICH_SERVER_DATA" envDefault:"false"`
	DisableBuiltinSeed       bool   `env:"DISABLE_BUILTIN_SEED" envDefault:""`
	Version                  string `env:"VERSION" envDefault:"dev"`
	GithubClientID           string `env:"GITHUB_CLIENT_ID" envDefault:""`
	GithubClientSecret       string `env:"GITHUB_CLIENT_SECRET" envDefault:""`
	JWTPrivateKey            string `env:"JWT_PRIVATE_KEY" envDefault:""`
	EnableAnonymousAuth      bool   `env:"ENABLE_ANONYMOUS_AUTH" envDefault:"false"`
	EnableRegistryValidation bool   `env:"ENABLE_REGISTRY_VALIDATION" envDefault:"true"`

	// OIDC Configuration
	OIDCEnabled      bool   `env:"OIDC_ENABLED" envDefault:"false"`
	OIDCIssuer       string `env:"OIDC_ISSUER" envDefault:""`
	OIDCClientID     string `env:"OIDC_CLIENT_ID" envDefault:""`
	OIDCExtraClaims  string `env:"OIDC_EXTRA_CLAIMS" envDefault:""`
	OIDCEditPerms    string `env:"OIDC_EDIT_PERMISSIONS" envDefault:""`
	OIDCPublishPerms string `env:"OIDC_PUBLISH_PERMISSIONS" envDefault:""`

	// Agent Gateway Configuration
	AgentGatewayPort uint16 `env:"AGENT_GATEWAY_PORT" envDefault:"8081"`

	// Runtime Configuration
	ReconcileOnStartup bool   `env:"RECONCILE_ON_STARTUP" envDefault:"true"`
	RuntimeDir         string `env:"RUNTIME_DIR" envDefault:"/tmp/arctl-runtime"`
	Verbose            bool   `env:"VERBOSE" envDefault:"false"`

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
}

// NewConfig creates a new configuration with default values
func NewConfig() *Config {
	err := godotenv.Load()
	if err != nil {
		log.Printf("No .env file found or error loading .env file: %v", err)
	}
	var cfg Config
	err = env.ParseWithOptions(&cfg, env.Options{
		Prefix: "AGENT_REGISTRY_",
	})
	if err != nil {
		log.Fatalf("failed to parse config: %v", err)
	}
	return &cfg
}

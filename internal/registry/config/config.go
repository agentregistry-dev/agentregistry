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
	DatabaseURL              string `env:"DATABASE_URL" envDefault:"postgres://localhost:5432/agent-registry?sslmode=disable"`
	SeedFrom                 string `env:"SEED_FROM" envDefault:""`
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

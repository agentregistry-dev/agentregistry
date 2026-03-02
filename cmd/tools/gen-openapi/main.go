package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"gopkg.in/yaml.v3"
)

func main() {
	outputPath := flag.String("output", "openapi.yaml", "Output path for OpenAPI spec")
	versionOverride := flag.String("version", "", "Override the API version (defaults to version.Version)")
	flag.Parse()

	apiVersion := version.Version
	if *versionOverride != "" {
		apiVersion = *versionOverride
	}

	spec := generateSpec(apiVersion)

	yamlData, err := yaml.Marshal(spec)
	if err != nil {
		log.Fatalf("Failed to marshal OpenAPI spec to YAML: %v", err)
	}

	if err := os.WriteFile(*outputPath, yamlData, 0644); err != nil {
		log.Fatalf("Failed to write OpenAPI spec to %s: %v", *outputPath, err)
	}

	absPath, err := filepath.Abs(*outputPath)
	if err != nil {
		absPath = *outputPath
	}
	fmt.Printf("OpenAPI spec generated: %s\n", absPath)
}

// generateSpec creates a Huma API, registers all routes, and returns the
// OpenAPI spec.
func generateSpec(apiVersion string) *huma.OpenAPI {
	mux := http.NewServeMux()

	humaConfig := huma.DefaultConfig("AgentRegistry", apiVersion)
	humaConfig.Info.Description = "AgentRegistry API for managing MCP servers, agents, skills, and deployments."
	// Disable $schema property injection in responses
	humaConfig.CreateHooks = []func(huma.Config) huma.Config{}

	api := humago.New(mux, humaConfig)

	// Some registration functions (auth handlers) dereference the config at
	// registration time to set up JWT managers, so we need a minimal config
	// with a valid dummy key. The key is never used for actual signing.
	cfg := &config.Config{
		JWTPrivateKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	// Register all routes. Service and metrics are nil because they are only
	// captured in handler closures and invoked at request time, not during
	// route registration.
	router.RegisterRoutes(api, cfg, nil, nil, nil, nil)

	return api.OpenAPI()
}

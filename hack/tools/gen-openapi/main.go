package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"sigs.k8s.io/yaml"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	arv1 "github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
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
	if err := applyGeneratedSpecSchemas(spec); err != nil {
		log.Fatalf("Failed to apply generated spec schemas: %v", err)
	}

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

func applyGeneratedSpecSchemas(spec *huma.OpenAPI) error {
	schemas := spec.Components.Schemas.Map()
	for _, descriptor := range arv1.KindDescriptors() {
		component := descriptor.Kind + "Spec"
		filename := strings.ToLower(descriptor.Kind) + ".yaml"
		data, err := os.ReadFile(filepath.Join(schemaDirectory(), filename))
		if err != nil {
			return err
		}
		var raw map[string]any
		if err := yaml.Unmarshal(data, &raw); err != nil {
			return err
		}
		schemas[component], err = humaSchema(raw)
		if err != nil {
			return fmt.Errorf("%s: %w", component, err)
		}
	}
	return nil
}

func schemaDirectory() string {
	pc, source, line, ok := runtime.Caller(0)
	if !ok || pc == 0 || line == 0 {
		panic("locate generated schemas")
	}
	return filepath.Join(filepath.Dir(source), "..", "..", "..", "pkg", "api", "v1alpha1", "schemas")
}

func humaSchema(raw map[string]any) (*huma.Schema, error) {
	extensions := make(map[string]any)
	plain := make(map[string]any, len(raw))
	for key, value := range raw {
		if len(key) > 2 && key[:2] == "x-" {
			extensions[key] = value
			continue
		}
		plain[key] = value
	}
	data, err := json.Marshal(plain)
	if err != nil {
		return nil, err
	}
	result := &huma.Schema{Extensions: extensions}
	if err := json.Unmarshal(data, result); err != nil {
		return nil, err
	}
	for name, property := range rawMap(raw["properties"]) {
		result.Properties[name], err = humaSchema(property)
		if err != nil {
			return nil, err
		}
	}
	if item, ok := raw["items"].(map[string]any); ok {
		result.Items, err = humaSchema(item)
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func rawMap(value any) map[string]map[string]any {
	properties, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	result := make(map[string]map[string]any, len(properties))
	for name, property := range properties {
		if property, ok := property.(map[string]any); ok {
			result[name] = property
		}
	}
	return result
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

	cfg := &config.Config{
		// Force-enable the read-only MCP Registry v0.1 compatibility routes so
		// the generated spec always documents them. The runtime default is OFF
		// (opt-in via AGENT_REGISTRY_MCP_REGISTRY_COMPAT_ENABLED); documenting
		// the surface regardless keeps the published OpenAPI complete.
		MCPRegistryCompatEnabled: true,
		// Same reasoning for the read-only Claude Code marketplace.json
		// compatibility route (opt-in via
		// AGENT_REGISTRY_PLUGIN_MARKETPLACE_COMPAT_ENABLED at runtime).
		PluginMarketplaceCompatEnabled: true,
	}

	// Register all routes. Services and metrics are nil because they are only
	// captured in handler closures and invoked at request time, not during
	// route registration.
	if err := router.RegisterRoutes(api, cfg, nil, &arv0.VersionBody{
		Version:   apiVersion,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildDate,
	}, &router.RouteOptions{
		Stores: v1alpha1store.NewStores(nil, pkgdb.OSSSchemaRegistry()),
	}); err != nil {
		panic(fmt.Sprintf("router.RegisterRoutes: %v", err))
	}

	return api.OpenAPI()
}

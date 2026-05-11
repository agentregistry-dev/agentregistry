package declarative

import "strings"

// providerEnvKeys is the canonical map from an Agent's model provider to the
// env keys arctl requires to be set before running. Mirrors what the
// docker-compose template wires in for the kagent ADK runtime.
//
// Single source of truth on the arctl side. The docker-compose / agent.py
// templates encode the same fact for runtime wiring; the duplication is
// intentional (CLI knows what to ask for; template knows what to plumb in).
var providerEnvKeys = map[string][]string{
	"gemini":       {"GOOGLE_API_KEY"},
	"openai":       {"OPENAI_API_KEY"},
	"anthropic":    {"ANTHROPIC_API_KEY"},
	"bedrock":      {"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION"},
	"agentgateway": nil, // local proxy, no user-provided auth
}

// ModelProviderEnvKeys returns the env keys an Agent of the given provider
// requires. Unknown providers return nil — arctl validates nothing extra and
// the runtime errors when constructing the model client. The escape hatch
// supports custom shims and providers added to templates ahead of this map.
func ModelProviderEnvKeys(provider string) []string {
	return providerEnvKeys[strings.ToLower(provider)]
}

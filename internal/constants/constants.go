// Package constants defines shared constant values used across the agentregistry codebase.
package constants

// Agent runtime environment variable keys injected by ResolveAgent and consumed
// by platform adapters and agent frameworks at deploy/run time.
const (
	// EnvKagentNamespace is the Kubernetes namespace where the agent is deployed.
	// Defaults to "default" when not explicitly set.
	EnvKagentNamespace = "KAGENT_NAMESPACE"

	// EnvKagentURL is the base URL of the kagent control plane the agent connects to.
	EnvKagentURL = "KAGENT_URL"

	// EnvKagentName is the registry name of the agent, used by the kagent runtime.
	EnvKagentName = "KAGENT_NAME"

	// EnvAgentName is the agent name exposed to the agent process itself.
	EnvAgentName = "AGENT_NAME"

	// EnvModelProvider is the LLM provider identifier (e.g. "openai", "anthropic").
	EnvModelProvider = "MODEL_PROVIDER"

	// EnvModelName is the model identifier within the provider (e.g. "gpt-4o").
	EnvModelName = "MODEL_NAME"

	// EnvMCPServersConfig is a JSON-encoded array of resolved MCP server
	// configurations injected into the agent container at deploy time.
	EnvMCPServersConfig = "MCP_SERVERS_CONFIG"
)

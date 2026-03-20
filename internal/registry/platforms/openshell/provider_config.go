package openshell

// OpenShellProviderConfig holds provider-level configuration for the OpenShell platform.
type OpenShellProviderConfig struct {
	GatewayName        string   `json:"gatewayName,omitempty"`
	InferenceProviders []string `json:"inferenceProviders,omitempty"`
}

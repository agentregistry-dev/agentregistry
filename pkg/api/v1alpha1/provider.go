package v1alpha1

// Provider is the typed envelope for kind=Provider resources. A Provider
// describes an execution target (local docker-compose daemon, a Kubernetes
// cluster, a hosted runtime) that Deployment resources reference via
// spec.providerRef.
type Provider struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	Spec     ProviderSpec `json:"spec" yaml:"spec"`
	Status   Status       `json:"status,omitzero" yaml:"status,omitempty"`
}

// Provider platform discriminators.
const (
	PlatformLocal      = "local"
	PlatformKubernetes = "kubernetes"
)

// ProviderSpec describes a deployment target. Platform is the discriminator;
// Config carries platform-specific configuration that downstream adapters
// (internal/registry/platforms/...) interpret. TelemetryEndpoint, when set,
// is exported to every Deployment served by this Provider as
// OTEL_EXPORTER_OTLP_ENDPOINT on the workload — telemetry is a property of
// where things run, not of an individual Deployment.
type ProviderSpec struct {
	Platform          string         `json:"platform" yaml:"platform"`
	Config            map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	TelemetryEndpoint string         `json:"telemetryEndpoint,omitempty" yaml:"telemetryEndpoint,omitempty"`
}

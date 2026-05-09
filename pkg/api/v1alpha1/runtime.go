package v1alpha1

// Runtime is the typed envelope for kind=Runtime resources. A Runtime
// describes an execution target (local docker daemon, a Kubernetes
// cluster, a hosted agent runtime) that Deployment resources reference
// via spec.runtimeRef.
type Runtime struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     RuntimeSpec `json:"spec" yaml:"spec"`
	Status   Status      `json:"status,omitzero" yaml:"status,omitempty"`
}

// Built-in runtime type discriminators. Canonical form is CamelCase.
// Manifests may write Spec.Type in any casing (`local`, `LOCAL`,
// `Local`); Runtime.Validate looks the input up case-insensitively in
// KnownRuntimeTypes and rewrites Spec.Type to the canonical CamelCase
// value at admission, so all downstream consumers compare against
// these constants with exact-match equality.
const (
	TypeLocal      = "Local"
	TypeKubernetes = "Kubernetes"
)

// RuntimeSpec describes a deployment target. Type is the discriminator;
// Config carries type-specific configuration that downstream adapters
// (internal/registry/runtimes/...) interpret. TelemetryEndpoint, when
// set, is exported to every Deployment served by this Runtime as
// OTEL_EXPORTER_OTLP_ENDPOINT on the workload — telemetry is a property
// of where things run, not of an individual Deployment.
type RuntimeSpec struct {
	Type              string         `json:"type" yaml:"type"`
	Config            map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	TelemetryEndpoint string         `json:"telemetryEndpoint,omitempty" yaml:"telemetryEndpoint,omitempty"`
}

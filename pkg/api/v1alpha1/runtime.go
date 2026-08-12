package v1alpha1

// Runtime is the typed envelope for kind=Runtime resources. A Runtime
// describes an execution target that Deployment resources reference via
// spec.runtimeRef. Concrete runtime types and their behavior are registered by
// applications embedding the registry.
type Runtime struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta  `json:"metadata" yaml:"metadata"`
	Spec     RuntimeSpec `json:"spec" yaml:"spec"`
	Status   Status      `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Runtime, RuntimeSpec](KindRuntime, WithMutableObjectStorage())
}

// TypeKubernetes is the canonical discriminator for Kubernetes runtime
// implementations supplied by embedding applications. The OSS registry does
// not register this type or provide a Kubernetes runtime implementation.
//
// Deprecated: use an application-owned runtime type.
const TypeKubernetes = "Kubernetes"

// RuntimeSpec describes a deployment target. Type is the discriminator;
// Config carries type-specific configuration that registered adapters
// interpret. TelemetryEndpoint, when
// set, is exported to every Deployment served by this Runtime as
// OTEL_EXPORTER_OTLP_ENDPOINT on the workload — telemetry is a property
// of where things run, not of an individual Deployment.
type RuntimeSpec struct {
	Type              string         `json:"type" yaml:"type"`
	Config            map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	TelemetryEndpoint string         `json:"telemetryEndpoint,omitempty" yaml:"telemetryEndpoint,omitempty"`
}

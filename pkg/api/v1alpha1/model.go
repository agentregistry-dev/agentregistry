package v1alpha1

// Model is the typed envelope for kind=Model resources. A Model is an
// admin-owned model definition: the model's identity (provider family and
// provider-scoped identifier) plus how the platform reaches and authenticates
// to it (endpoint, auth posture, secret refs).
type Model struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     ModelSpec  `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Model, ModelSpec](KindModel, WithMutableObjectStorage())
}

// Supported provider families. Expand this enum only when the provider has a
// working runtime adapter and end-to-end coverage.
const (
	ModelProviderBedrock = "bedrock"
)

// Model auth strategies. See ModelAuthConfig.
const (
	ModelAuthStrategyRuntime     = "runtime"
	ModelAuthStrategySecretRef   = "secretRef"
	ModelAuthStrategyPassthrough = "passthrough"
)

// ModelSpec describes one model: catalog display metadata,
// provider-scoped identity, and platform-owned connection posture.
//
// Model is a mutable namespace/name object (no tags): auth and endpoint edits
// are routine config mutations, not new versions.
type ModelSpec struct {
	// Catalog display metadata.
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Provider family. Currently only "bedrock" is supported.
	Provider string `json:"provider" yaml:"provider" enum:"bedrock"`

	// Model is the provider-scoped model identifier, e.g.
	// "us.anthropic.claude-opus-4-8".
	Model string `json:"model" yaml:"model"`

	// Auth is how the platform authenticates to the provider. Omitted means
	// the provider default: ambient runtime identity for Bedrock.
	Auth *ModelAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`

	// Endpoint overrides how the provider is reached. Omitted means
	// provider defaults.
	Endpoint *ModelEndpointConfig `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// ModelAuthConfig declares the auth posture for reaching the provider. OSS
// stores SecretRef opaquely and never resolves it; resolution is owned by
// distributions with a secret store.
type ModelAuthConfig struct {
	// Strategy is "runtime" (ambient cloud identity), "secretRef" (key
	// material from a registry Secret), or "passthrough" (inbound bearer
	// token forwarded as the provider key).
	Strategy string `json:"strategy" yaml:"strategy" enum:"runtime,secretRef,passthrough"`
	// SecretRef is required iff Strategy == "secretRef".
	SecretRef *SecretKeyRef `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
}

// ModelEndpointConfig overrides how the provider is reached.
type ModelEndpointConfig struct {
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	// Region overrides the model-endpoint region (bedrock); empty means the
	// provider default.
	Region string          `json:"region,omitempty" yaml:"region,omitempty"`
	TLS    *ModelTLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// ModelTLSConfig carries TLS settings for private gateway endpoints.
type ModelTLSConfig struct {
	// CACertSecretRef names CA material for private gateways.
	CACertSecretRef *SecretKeyRef `json:"caCertSecretRef,omitempty" yaml:"caCertSecretRef,omitempty"`
	// DisableVerify is for dev/test only.
	DisableVerify bool `json:"disableVerify,omitempty" yaml:"disableVerify,omitempty"`
}

// SecretKeyRef names a key in a registry Secret. OSS stores and structurally
// validates it but never resolves it. Secret values are not stored on Model
// resources.
type SecretKeyRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key,omitempty" yaml:"key,omitempty"`
}

package v1alpha1

// Model is the typed envelope for kind=Model resources. A Model is an
// admin-owned model definition: the model's identity (provider family and
// provider-scoped identifier) plus how the platform reaches and authenticates
// to it (endpoint, auth posture, secret refs).
type Model struct {
	TypeMeta `json:",inline" yaml:",inline"`
	// metadata is part of Model.
	// +required
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	// spec is part of Model.
	// +required
	Spec ModelSpec `json:"spec" yaml:"spec"`
	// status is part of Model.
	// +optional
	Status Status `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Model, ModelSpec](KindModel)
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
// Model is a tagged catalog artifact. Provider identity and platform-owned
// auth/endpoint posture are versioned together so Deployments can pin the
// complete model configuration they consume.
type ModelSpec struct {
	// title is the model's catalog display name.
	// +optional
	Title string `json:"title,omitempty" yaml:"title,omitempty"`
	// description is part of ModelSpec.
	// +optional
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// iconUrl is the image a catalog UI shows for this model. Either an
	// absolute https:// URL or a root-relative path served by the UI.
	// +optional
	IconURL string `json:"iconUrl,omitempty" yaml:"iconUrl,omitempty"`

	// provider family. Currently only "bedrock" is supported.
	// +required
	// +kubebuilder:validation:MinLength=1
	Provider string `json:"provider" yaml:"provider" enum:"bedrock"`

	// model is the provider-scoped model identifier, e.g.
	// "us.anthropic.claude-opus-4-8".
	// +required
	// +kubebuilder:validation:MinLength=1
	Model string `json:"model" yaml:"model"`

	// auth is how the platform authenticates to the provider. Omitted means
	// the provider default: ambient runtime identity for Bedrock.
	// +optional
	Auth *ModelAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`

	// endpoint overrides how the provider is reached. Omitted means
	// provider defaults.
	// +optional
	Endpoint *ModelEndpointConfig `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// ModelAuthConfig declares the auth posture for reaching the provider. OSS
// stores SecretRef opaquely and never resolves it; resolution is owned by
// distributions with a secret store.
type ModelAuthConfig struct {
	// strategy is "runtime" (ambient cloud identity), "secretRef" (key
	// material from a registry Secret), or "passthrough" (inbound bearer
	// token forwarded as the provider key).
	// +required
	// +kubebuilder:validation:MinLength=1
	Strategy string `json:"strategy" yaml:"strategy" enum:"runtime,secretRef,passthrough"`
	// secretRef is required iff Strategy == "secretRef".
	// +optional
	SecretRef *SecretKeyRef `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
}

// ModelEndpointConfig overrides how the provider is reached.
type ModelEndpointConfig struct {
	// baseUrl is part of ModelEndpointConfig.
	// +optional
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	// region overrides the model-endpoint region (bedrock); empty means the
	// provider default.
	// +optional
	Region string `json:"region,omitempty" yaml:"region,omitempty"`
	// tls is part of ModelEndpointConfig.
	// +optional
	TLS *ModelTLSConfig `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// ModelTLSConfig carries TLS settings for private gateway endpoints.
type ModelTLSConfig struct {
	// caCertSecretRef names CA material for private gateways.
	// +optional
	CACertSecretRef *SecretKeyRef `json:"caCertSecretRef,omitempty" yaml:"caCertSecretRef,omitempty"`
	// disableVerify is for dev/test only.
	// +optional
	DisableVerify bool `json:"disableVerify,omitempty" yaml:"disableVerify,omitempty"`
}

// SecretKeyRef names a key in a registry Secret. OSS stores and structurally
// validates it but never resolves it. Secret values are not stored on Model
// resources.
type SecretKeyRef struct {
	// namespace is part of SecretKeyRef.
	// +optional
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// name is part of SecretKeyRef.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name" yaml:"name"`
	// key is part of SecretKeyRef.
	// +optional
	Key string `json:"key,omitempty" yaml:"key,omitempty"`
}

package v1alpha1

// Model is the typed envelope for kind=Model resources. A Model is an
// admin-owned, deployable model definition: the model's identity (provider
// family + provider-scoped identifier) plus how the platform reaches and
// authenticates to it (endpoint, auth posture, secret refs). Deployment
// resources select one via spec.modelRef; deployers cannot override
// endpoint or auth at deployment time.
type Model struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta `json:"metadata" yaml:"metadata"`
	Spec     ModelSpec  `json:"spec" yaml:"spec"`
	Status   Status     `json:"status,omitzero" yaml:"status,omitempty"`
}

func init() {
	MustRegisterKind[*Model, ModelSpec](KindModel, WithMutableObjectStorage())
}

// Known provider families. Model identifiers are provider-scoped (a Bedrock
// model ID is not an Anthropic API ID), so Provider disambiguates how the
// identifier and auth material are interpreted downstream.
const (
	ModelProviderBedrock   = "bedrock"
	ModelProviderAnthropic = "anthropic"
	ModelProviderOpenAI    = "openai"
	ModelProviderVertex    = "vertex"
)

// Model auth strategies. See ModelAuthConfig.
const (
	ModelAuthStrategyRuntime     = "runtime"
	ModelAuthStrategySecretRef   = "secretRef"
	ModelAuthStrategyPassthrough = "passthrough"
)

// ModelSpec describes one deployable model: catalog display metadata,
// provider-scoped identity, and platform-owned connection posture.
//
// Model is a mutable namespace/name object (no tags), like Runtime and
// Deployment: auth/endpoint edits are routine config mutations that must
// propagate to referencing Deployments through the controller's
// dependency handling.
type ModelSpec struct {
	// Catalog display metadata.
	Title       string `json:"title,omitempty" yaml:"title,omitempty"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Provider family: "bedrock", "anthropic", "openai", "vertex".
	Provider string `json:"provider" yaml:"provider"`

	// Model is the provider-scoped model identifier, e.g.
	// "us.anthropic.claude-opus-4-8".
	Model string `json:"model" yaml:"model"`

	// Auth is how the platform authenticates to the provider. Omitted
	// means the provider default: "runtime" for ambient-identity
	// providers (bedrock, vertex); key-based providers (anthropic,
	// openai) must declare a strategy.
	Auth *ModelAuthConfig `json:"auth,omitempty" yaml:"auth,omitempty"`

	// Endpoint overrides how the provider is reached. Omitted means
	// provider defaults.
	Endpoint *ModelEndpointConfig `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
}

// ModelAuthConfig declares the auth posture for reaching the provider.
// Resolution is deployment-time and distribution-specific: OSS stores
// SecretRef opaquely and never resolves it; distributions with a secret
// store resolve it in-process at deploy time.
type ModelAuthConfig struct {
	// Strategy is "runtime" (ambient cloud identity), "secretRef" (key
	// material from a registry Secret), or "passthrough" (inbound bearer
	// token forwarded as the provider key).
	Strategy string `json:"strategy,omitempty" yaml:"strategy,omitempty"`
	// SecretRef is required iff Strategy == "secretRef".
	SecretRef *SecretKeyRef `json:"secretRef,omitempty" yaml:"secretRef,omitempty"`
}

// ModelEndpointConfig overrides how the provider is reached.
type ModelEndpointConfig struct {
	BaseURL string `json:"baseUrl,omitempty" yaml:"baseUrl,omitempty"`
	// Region overrides the model-endpoint region (bedrock); empty means
	// the deployment runtime's region.
	Region         string            `json:"region,omitempty" yaml:"region,omitempty"`
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty" yaml:"defaultHeaders,omitempty"`
	TLS            *ModelTLSConfig   `json:"tls,omitempty" yaml:"tls,omitempty"`
}

// ModelTLSConfig carries TLS settings for private gateway endpoints.
type ModelTLSConfig struct {
	// CACertSecretRef names CA material for private gateways.
	CACertSecretRef *SecretKeyRef `json:"caCertSecretRef,omitempty" yaml:"caCertSecretRef,omitempty"`
	// DisableVerify is for dev/test only.
	DisableVerify bool `json:"disableVerify,omitempty" yaml:"disableVerify,omitempty"`
}

// SecretKeyRef names a key in a registry Secret. OSS stores and
// structurally validates it but never resolves it; distributions with a
// Secret store (enterprise) resolve it in-process at deploy time. Secret
// values are write-only and never served.
type SecretKeyRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key,omitempty" yaml:"key,omitempty"`
}

// ModelRef selects a Model by namespace/name. Kind is implicit (always
// Model) and there is no tag because Model is a mutable-object kind.
//
// Namespace is optional: blank means "same namespace as the referencing
// object".
type ModelRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
}

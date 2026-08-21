package v1alpha1

// ResourceRef is a typed reference to another resource in the registry.
// Public references use one shape across v1alpha1: {Kind, Namespace, Name,
// Tag}. Tag is meaningful only for tagged resources.
//
// Namespace is optional: blank means "same namespace as the referencing
// object" (the common case). Tag is optional: blank means "resolve to the
// literal latest tag" for tagged resources or "resolve by namespace/name"
// for untagged resource kinds.
type ResourceRef struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

// DeploymentRef is a typed reference to another Deployment resource. Kind
// is implicit (always Deployment) and Tag is omitted because Deployment is
// an untagged kind keyed by namespace/name.
//
// Namespace is optional: blank means "same namespace as the referencing
// Deployment".
type DeploymentRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
}

// DefaultModelName is the conventional namespace-scoped Model selected by a
// harness Agent Deployment that omits spec.modelRef. Its blank tag resolves the
// literal "latest" tag, so the complete implicit identity is
// Model/<deployment namespace>/default@latest.
const DefaultModelName = "default"

// ModelRef selects a tagged Model. Kind is implicit (always Model).
//
// Namespace is optional: blank means "same namespace as the referencing
// Deployment". Tag is optional: blank resolves the literal "latest" tag. When
// a harness Agent Deployment omits ModelRef entirely, it defaults to
// {name: "default"} in the Deployment namespace.
type ModelRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

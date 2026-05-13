package v1alpha1

// ResourceRef is a typed reference to another resource in the registry.
// Public references use one shape across v1alpha1: {Kind, Namespace, Name,
// Tag}. Tag is meaningful only for taggable registry artifacts.
//
// Namespace is optional: blank means "same namespace as the referencing
// object" (the common case). Tag is optional: blank means "resolve to the
// literal latest tag" for taggable artifacts or "resolve by namespace/name"
// for mutable object kinds.
type ResourceRef struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

// DeploymentRef is a typed reference to another Deployment resource. Kind
// is implicit (always Deployment) and Tag is omitted because Deployment is
// a mutable-object kind keyed by namespace/name.
//
// Namespace is optional: blank means "same namespace as the referencing
// Deployment".
type DeploymentRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
}

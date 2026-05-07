package v1alpha1

// ResourceRef is a typed reference to another resource in the registry.
// Content-registry references use {Kind, Namespace, Name, Tag}; legacy
// infra/config references may still use Version.
//
// Namespace is optional: blank means "same namespace as the referencing
// object" (the common case). Tag is optional: blank means "resolve to the
// literal latest tag" at reference-resolution time.
type ResourceRef struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
	// Version is retained for legacy Provider/Deployment refs. Content
	// refs should use Tag.
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

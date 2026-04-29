package v1alpha1

// ResourceRef is a typed reference to another resource in the registry.
// Every reference is {Kind, Namespace, Name, Version}.
//
// Namespace is optional: blank means "same namespace as the referencing
// object" (the common case). Version is optional: blank means "resolve to
// latest" at reference-resolution time.
type ResourceRef struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Version   string `json:"version,omitempty" yaml:"version,omitempty"`
}

package v1alpha1

// ResourceRef is a typed reference to another resource in the registry.
// It replaces the legacy inline resource definitions (McpServerType with
// Type/Command/Args/URL/Headers/Image/Build, RegistrySkillName, etc.) — every
// reference is now {Kind, Name, Version}. An empty Version means "latest".
type ResourceRef struct {
	Kind    string `json:"kind" yaml:"kind"`
	Name    string `json:"name" yaml:"name"`
	Version string `json:"version,omitempty" yaml:"version,omitempty"`
}

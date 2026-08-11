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

// ComponentRef is a reference to a registry artifact whose kind is fixed by
// the field holding it (e.g. PluginSpec.Skills refs are always Kind=Skill).
// Unlike ResourceRef it carries no Kind: the schema already determines it, so
// a kind field would be pure redundancy plus a defaulting/mismatch surface.
//
// Namespace is optional: blank means "same namespace as the referencing
// object". Tag is optional: blank means "resolve to the literal latest tag".
type ComponentRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
}

// AsResourceRef adapts the ref for machinery that speaks ResourceRef,
// supplying the kind the holding field implies and defaulting a blank
// namespace to fallbackNamespace.
func (r ComponentRef) AsResourceRef(kind, fallbackNamespace string) ResourceRef {
	ns := r.Namespace
	if ns == "" {
		ns = fallbackNamespace
	}
	return ResourceRef{Kind: kind, Namespace: ns, Name: r.Name, Tag: r.Tag}
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

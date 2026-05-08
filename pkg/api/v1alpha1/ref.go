package v1alpha1

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

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

	// Version is a deprecated input alias retained for in-process callers and
	// old manifests. It is normalized to Tag while decoding and is never
	// emitted on the wire.
	Version string `json:"-" yaml:"-"`
}

type resourceRefWire struct {
	Kind      string `json:"kind" yaml:"kind"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Tag       string `json:"tag,omitempty" yaml:"tag,omitempty"`
	Version   string `json:"version,omitempty" yaml:"version,omitempty"`
}

// MarshalJSON emits the canonical v1alpha1 reference shape and never writes
// the deprecated version alias.
func (r ResourceRef) MarshalJSON() ([]byte, error) {
	return json.Marshal(resourceRefWire{
		Kind:      r.Kind,
		Namespace: r.Namespace,
		Name:      r.Name,
		Tag:       r.Tag,
	})
}

// MarshalYAML emits the canonical v1alpha1 reference shape and never writes the
// deprecated version alias.
func (r ResourceRef) MarshalYAML() (any, error) {
	return resourceRefWire{
		Kind:      r.Kind,
		Namespace: r.Namespace,
		Name:      r.Name,
		Tag:       r.Tag,
	}, nil
}

// UnmarshalJSON accepts the deprecated version alias at the public boundary
// and normalizes it immediately to Tag. Validation later decides whether the
// referenced kind is allowed to use a tag.
func (r *ResourceRef) UnmarshalJSON(data []byte) error {
	var w resourceRefWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	r.applyWire(w)
	return nil
}

// UnmarshalYAML accepts the deprecated version alias at the manifest boundary
// and normalizes it immediately to Tag.
func (r *ResourceRef) UnmarshalYAML(value *yaml.Node) error {
	var w resourceRefWire
	if err := value.Decode(&w); err != nil {
		return err
	}
	r.applyWire(w)
	return nil
}

func (r *ResourceRef) applyWire(w resourceRefWire) {
	r.Kind = w.Kind
	r.Namespace = w.Namespace
	r.Name = w.Name
	r.Tag = w.Tag
	if r.Tag == "" {
		r.Tag = w.Version
	}
	r.Version = ""
}

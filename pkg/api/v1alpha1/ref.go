package v1alpha1

import (
	"encoding/json"
	"errors"
	"strings"

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
}

type resourceRefWire ResourceRef

var errDeprecatedResourceRefVersion = errors.New("version is deprecated; use tag")

// UnmarshalJSON rejects the deprecated version alias instead of silently
// ignoring it.
func (r *ResourceRef) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	for key := range raw {
		if strings.EqualFold(key, "version") {
			return errDeprecatedResourceRefVersion
		}
	}

	var w resourceRefWire
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*r = ResourceRef(w)
	return nil
}

// UnmarshalYAML rejects the deprecated version alias instead of silently
// ignoring it.
func (r *ResourceRef) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(value.Content); i += 2 {
			if strings.EqualFold(value.Content[i].Value, "version") {
				return errDeprecatedResourceRefVersion
			}
		}
	}

	var w resourceRefWire
	if err := value.Decode(&w); err != nil {
		return err
	}
	*r = ResourceRef(w)
	return nil
}

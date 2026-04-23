package scheme

import (
	"fmt"
	"os"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"gopkg.in/yaml.v3"
)

// APIVersion is the canonical apiVersion string for arctl declarative YAML files.
const APIVersion = v1alpha1.GroupVersion

// Resource is the CLI decode result for a single declarative document.
// Spec is always a pointer to the concrete v1alpha1 spec struct for the decoded kind.
// Status is intentionally dropped on decode so `get -o yaml | apply -f -` stays
// apply-safe even when the source YAML contained server-managed status.
type Resource struct {
	Object     v1alpha1.Object
	APIVersion string
	Kind       string
	Metadata   v1alpha1.ObjectMeta
	Spec       any
	Status     any
}

// IsEnvelopeYAML reports whether the given bytes look like a declarative
// ar.dev/v1alpha1 envelope. Malformed YAML returns false so callers can fall
// back to legacy manifest parsing paths that surface the real error later.
func IsEnvelopeYAML(data []byte) bool {
	var header struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return false
	}
	return header.APIVersion != "" && header.Kind != ""
}

// DecodeBytes parses one or more declarative YAML documents using the provided registry.
// Returns an error if any document has an unknown kind or an unparseable spec.
func DecodeBytes(reg *Registry, b []byte) ([]*Resource, error) {
	if reg == nil {
		return nil, fmt.Errorf("scheme: registry is required")
	}
	decoded, err := v1alpha1.Default.DecodeMulti(b)
	if err != nil {
		return nil, err
	}
	out := make([]*Resource, 0, len(decoded))
	for _, item := range decoded {
		obj, ok := item.(v1alpha1.Object)
		if !ok {
			return nil, fmt.Errorf("scheme: decoded value does not implement v1alpha1.Object: %T", item)
		}
		kindDef, err := reg.Lookup(obj.GetKind())
		if err != nil {
			return nil, err
		}
		obj.SetStatus(v1alpha1.Status{})
		out = append(out, resourceFromObject(kindDef.Kind, obj))
	}
	return out, nil
}

// DecodeFile reads a YAML file and decodes it using the provided registry.
func DecodeFile(reg *Registry, path string) ([]*Resource, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(reg, b)
}

func resourceFromObject(kind string, obj v1alpha1.Object) *Resource {
	resource := &Resource{
		Object:     obj,
		APIVersion: obj.GetAPIVersion(),
		Kind:       kind,
		Metadata:   *obj.GetMetadata(),
	}

	switch typed := obj.(type) {
	case *v1alpha1.Agent:
		resource.Spec = &typed.Spec
	case *v1alpha1.MCPServer:
		resource.Spec = &typed.Spec
	case *v1alpha1.Skill:
		resource.Spec = &typed.Spec
	case *v1alpha1.Prompt:
		resource.Spec = &typed.Spec
	case *v1alpha1.Provider:
		resource.Spec = &typed.Spec
	case *v1alpha1.Deployment:
		resource.Spec = &typed.Spec
	default:
		resource.Spec = obj
	}
	return resource
}

func kindAliasKey(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

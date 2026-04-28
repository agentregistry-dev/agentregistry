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

// IsEnvelopeYAML reports whether the given bytes look like a declarative
// ar.dev/v1alpha1 envelope. Malformed YAML returns false so callers can
// surface the real parse error from a downstream loader.
//
// Pins on the canonical apiVersion (ar.dev/v1alpha1) — generic K8s-style
// manifests with an unrelated apiVersion (kagent CRDs, k8s core types, etc.)
// would otherwise misclassify as envelopes and route to the wrong loader.
func IsEnvelopeYAML(data []byte) bool {
	var header struct {
		APIVersion string `yaml:"apiVersion"`
		Kind       string `yaml:"kind"`
	}
	if err := yaml.Unmarshal(data, &header); err != nil {
		return false
	}
	return header.APIVersion == APIVersion && header.Kind != ""
}

// DecodeBytes parses one or more declarative YAML documents into typed
// v1alpha1.Object envelopes. Returns an error if any document has an
// unknown kind or fails to decode.
//
// Status is reset to nil before returning so `arctl get -o yaml | apply
// -f -` stays apply-safe even when the source YAML contained
// server-managed status.
func DecodeBytes(b []byte) ([]v1alpha1.Object, error) {
	decoded, err := v1alpha1.Default.DecodeMulti(b)
	if err != nil {
		return nil, err
	}
	out := make([]v1alpha1.Object, 0, len(decoded))
	for _, item := range decoded {
		obj, ok := item.(v1alpha1.Object)
		if !ok {
			return nil, fmt.Errorf("scheme: decoded value does not implement v1alpha1.Object: %T", item)
		}
		if _, err := Lookup(obj.GetKind()); err != nil {
			return nil, err
		}
		if err := obj.UnmarshalStatus(nil); err != nil {
			return nil, fmt.Errorf("reset status: %w", err)
		}
		out = append(out, obj)
	}
	return out, nil
}

// DecodeFile reads a YAML file and decodes it into v1alpha1.Object envelopes.
func DecodeFile(path string) ([]v1alpha1.Object, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(b)
}

func kindAliasKey(kind string) string {
	trimmed := strings.TrimSpace(kind)
	if trimmed == "" {
		return ""
	}
	return strings.ToLower(trimmed)
}

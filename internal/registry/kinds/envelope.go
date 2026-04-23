package kinds

import "gopkg.in/yaml.v3"

// EnvelopeHeader carries only the two top-level fields needed to classify a
// YAML document as a declarative ar.dev/v1alpha1 envelope. Used by CLI-side
// manifest loaders to detect envelope YAML before deciding whether to route
// through the declarative decode path or a legacy flat-manifest decoder.
type EnvelopeHeader struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
}

// IsEnvelopeYAML reports whether the given bytes look like a declarative
// ar.dev/v1alpha1 envelope (both apiVersion and kind are present at the top
// level). Malformed YAML returns false so callers can fall back to a legacy
// path that will surface the real parse error.
func IsEnvelopeYAML(data []byte) bool {
	var h EnvelopeHeader
	if err := yaml.Unmarshal(data, &h); err != nil {
		return false
	}
	return h.APIVersion != "" && h.Kind != ""
}

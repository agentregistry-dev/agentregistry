package v1alpha1store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// SpecHash returns a deterministic SHA-256 hex digest of a JSON spec.
// Field order and whitespace do not affect the result; only the content
// (keys + values) does. Empty/null specs hash to a stable sentinel.
func SpecHash(raw json.RawMessage) string {
	if len(raw) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	// canonical-by-Marshal: encoding/json sorts map keys deterministically.
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Fallback: hash raw bytes. Apply will surface decode errors elsewhere.
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	canonical, err := json.Marshal(v)
	if err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

const DefaultTagValue = "latest"

// ContentHash returns the canonical digest used for same-tag replacement
// detection. It deliberately includes only user-authored declarative state:
// spec plus labels/annotations.
func ContentHash(meta *v1alpha1.ObjectMeta, spec json.RawMessage) (string, error) {
	payload := struct {
		Metadata struct {
			Labels      map[string]string `json:"labels,omitempty"`
			Annotations map[string]string `json:"annotations,omitempty"`
		} `json:"metadata"`
		Spec any `json:"spec"`
	}{}
	if meta != nil {
		payload.Metadata.Labels = meta.Labels
		payload.Metadata.Annotations = meta.Annotations
	}
	if len(spec) > 0 {
		if err := json.Unmarshal(spec, &payload.Spec); err != nil {
			return "", err
		}
	}
	canonical, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:]), nil
}

// DefaultTag returns the tag assigned when metadata.tag is omitted.
func DefaultTag() string {
	return DefaultTagValue
}

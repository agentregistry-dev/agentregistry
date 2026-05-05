package v1alpha1store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// SpecHash returns a deterministic SHA-256 hex digest of a JSON spec.
// Field order and whitespace do not affect the result; only the content
// (keys + values) does. Empty/null specs hash to a stable sentinel.
func SpecHash(raw json.RawMessage) string {
	if len(raw) == 0 {
		sum := sha256.Sum256(nil)
		return hex.EncodeToString(sum[:])
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		// Fallback: hash raw bytes. Apply will surface decode errors elsewhere.
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	canonical, err := json.Marshal(canonicalize(v))
	if err != nil {
		sum := sha256.Sum256(raw)
		return hex.EncodeToString(sum[:])
	}
	sum := sha256.Sum256(canonical)
	return hex.EncodeToString(sum[:])
}

// canonicalize sorts map keys recursively. json.Marshal already sorts keys
// at serialization time, but doing it explicitly makes the contract obvious
// and lets us add stable null-handling later if needed.
func canonicalize(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, vv := range x {
			m[k] = canonicalize(vv)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, vv := range x {
			out[i] = canonicalize(vv)
		}
		return out
	default:
		return x
	}
}

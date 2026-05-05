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

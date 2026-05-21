package v1alpha1

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// ConditionStatus values, matching Kubernetes apimachinery/pkg/apis/meta/v1.
type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// Condition describes one facet of a resource's observed state. Modeled
// after Kubernetes v1.Condition: Type is the named condition
// (e.g. "Ready", "Validated", "Published"); Status is True/False/Unknown;
// Reason is a machine-readable CamelCase token; Message is a
// human-readable explanation; LastTransitionTime is when Status last
// flipped.
//
// ObservedGeneration is the spec generation this condition was derived from.
// Like ObjectMeta.Generation it is an internal reconciler convergence signal:
// kept on the struct for controllers to read, persisted in storage, but hidden
// from the wire so the user-facing metadata surface stays minimal.
type Condition struct {
	Type               string          `json:"type" yaml:"type"`
	Status             ConditionStatus `json:"status" yaml:"status"`
	Reason             string          `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string          `json:"message,omitempty" yaml:"message,omitempty"`
	LastTransitionTime time.Time       `json:"lastTransitionTime,omitzero" yaml:"lastTransitionTime,omitempty"`
	ObservedGeneration int64           `json:"-" yaml:"-"`
}

// Status is the observed-state subresource. ObservedGeneration is the highest
// metadata.generation any reconciler has acted on; Conditions is the list of
// fine-grained state facets written by the reconciler and service layer. No
// Phase roll-up — K8s deprecated it in favor of Conditions, and carrying a
// string summary encourages downstream string-comparison anti-patterns.
//
// ObservedGeneration is internal-only (matches ObjectMeta.Generation).
type Status struct {
	ObservedGeneration int64       `json:"-" yaml:"-"`
	Conditions         []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`

	// Details is an opaque JSON object populated by runtime adapters that need
	// to surface structured state beyond what Conditions can express. Each
	// adapter owns its own top-level key inside Details; consumers parse only
	// the keys they care about. Empty when no adapter has written.
	//
	// Use SetDetailsKey / GetDetailsKey to merge or read keys without clobbering
	// other adapters' state.
	Details json.RawMessage `json:"details,omitempty" yaml:"details,omitempty"`
}

// SetDetailsKey merges value (as JSON) under key in s.Details. Other top-level keys
// in s.Details are preserved; a nil value removes the key. Returns an error if
// value cannot be marshaled or if existing Details is not a JSON object.
func (s *Status) SetDetailsKey(key string, value any) error {
	if key == "" {
		return errors.New("status: SetDetailsKey key must not be empty")
	}
	m := map[string]json.RawMessage{}
	if len(s.Details) > 0 {
		if err := json.Unmarshal(s.Details, &m); err != nil {
			return fmt.Errorf("status: existing Details is not a JSON object: %w", err)
		}
	}
	if value == nil {
		delete(m, key)
	} else {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("status: marshal %q: %w", key, err)
		}
		m[key] = encoded
	}
	if len(m) == 0 {
		s.Details = nil
		return nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("status: marshal Details: %w", err)
	}
	s.Details = out
	return nil
}

// SetDetailsKeyJSON is SetDetailsKey for callers that already hold the value as
// pre-encoded JSON bytes. Skips the marshal step so byte equality is
// preserved (useful when the caller wants the on-disk JSON to match a
// canonical form). encoded must be a valid JSON value; pass nil to remove
// the key.
func (s *Status) SetDetailsKeyJSON(key string, encoded json.RawMessage) error {
	if key == "" {
		return errors.New("status: SetDetailsKeyJSON key must not be empty")
	}
	m := map[string]json.RawMessage{}
	if len(s.Details) > 0 {
		if err := json.Unmarshal(s.Details, &m); err != nil {
			return fmt.Errorf("status: existing Details is not a JSON object: %w", err)
		}
	}
	if len(encoded) == 0 {
		delete(m, key)
	} else {
		// Validate encoded is well-formed before storing.
		if !json.Valid(encoded) {
			return fmt.Errorf("status: SetDetailsKeyJSON(%q): value is not valid JSON", key)
		}
		m[key] = encoded
	}
	if len(m) == 0 {
		s.Details = nil
		return nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("status: marshal Details: %w", err)
	}
	s.Details = out
	return nil
}

// GetDetailsKey unmarshals the value at key in s.Details into out. Returns (false,
// nil) when the key is absent. Returns an error if Details is malformed or out
// cannot receive the value.
func (s *Status) GetDetailsKey(key string, out any) (bool, error) {
	if len(s.Details) == 0 || key == "" {
		return false, nil
	}
	m := map[string]json.RawMessage{}
	if err := json.Unmarshal(s.Details, &m); err != nil {
		return false, fmt.Errorf("status: existing Details is not a JSON object: %w", err)
	}
	v, ok := m[key]
	if !ok {
		return false, nil
	}
	if err := json.Unmarshal(v, out); err != nil {
		return false, fmt.Errorf("status: unmarshal %q: %w", key, err)
	}
	return true, nil
}

// SetCondition adds or updates the condition matching c.Type on s. If an entry
// exists and its Status matches c.Status, the existing LastTransitionTime is
// preserved; otherwise LastTransitionTime is set to now (or c.LastTransitionTime
// if non-zero). Reason and Message are always overwritten.
func (s *Status) SetCondition(c Condition) {
	now := c.LastTransitionTime
	if now.IsZero() {
		now = time.Now().UTC()
	}
	for i, existing := range s.Conditions {
		if existing.Type != c.Type {
			continue
		}
		if existing.Status == c.Status {
			c.LastTransitionTime = existing.LastTransitionTime
		} else {
			c.LastTransitionTime = now
		}
		s.Conditions[i] = c
		return
	}
	c.LastTransitionTime = now
	s.Conditions = append(s.Conditions, c)
}

// GetCondition returns a pointer to the condition with the matching Type, or
// nil if none exists. The returned pointer aliases the slice element, so
// callers must not mutate through it while holding the Status.
func (s *Status) GetCondition(conditionType string) *Condition {
	for i := range s.Conditions {
		if s.Conditions[i].Type == conditionType {
			return &s.Conditions[i]
		}
	}
	return nil
}

// IsConditionTrue reports whether the condition with the given Type exists
// and has Status == ConditionTrue.
func (s *Status) IsConditionTrue(conditionType string) bool {
	c := s.GetCondition(conditionType)
	return c != nil && c.Status == ConditionTrue
}

// conditionStore is the on-disk shape of a Condition. It serializes
// ObservedGeneration for controller state while the public wire shape keeps
// that field hidden.
type conditionStore struct {
	Type               string          `json:"type"`
	Status             ConditionStatus `json:"status"`
	Reason             string          `json:"reason,omitempty"`
	Message            string          `json:"message,omitempty"`
	LastTransitionTime time.Time       `json:"lastTransitionTime,omitzero"`
	ObservedGeneration int64           `json:"observedGeneration,omitempty"`
}

// statusStore is the on-disk shape of Status. Mirrors Status, but with
// ObservedGeneration visible to the JSON encoder. See MarshalStatusForStorage /
// UnmarshalStatusFromStorage.
type statusStore struct {
	ObservedGeneration int64            `json:"observedGeneration,omitempty"`
	Conditions         []conditionStore `json:"conditions,omitempty"`
	Details            json.RawMessage  `json:"details,omitempty"`
}

// MarshalStatusForStorage serializes a Status to JSON suitable for
// writing to the status JSONB column. Routed through the storage shapes
// so storage-only fields (if any are added later) survive the round
// trip independently of the wire schema.
func MarshalStatusForStorage(s Status) ([]byte, error) {
	// Condition and conditionStore have identical fields (just different
	// json tags) so a direct conversion is safe and beats a manual copy.
	storeConds := make([]conditionStore, len(s.Conditions))
	for i, c := range s.Conditions {
		storeConds[i] = conditionStore(c)
	}
	return json.Marshal(statusStore{
		ObservedGeneration: s.ObservedGeneration,
		Conditions:         storeConds,
		Details:            s.Details,
	})
}

// StatusPatcher adapts a typed Status mutator into the opaque-bytes
// signature that v1alpha1store.PatchOpts.Status / Store.PatchStatus
// expect. Callers that use the typed v1alpha1.Status schema wrap their
// SetCondition / ObservedGeneration logic here:
//
//	store.PatchStatus(ctx, ns, name, tag, v1alpha1.StatusPatcher(
//	    func(s *v1alpha1.Status) {
//	        s.ObservedGeneration = gen
//	        s.SetCondition(v1alpha1.Condition{Type: "Ready", Status: v1alpha1.ConditionTrue})
//	    },
//	))
//
// Kinds with a custom status shape skip this helper and return their
// own marshaled bytes directly from the PatchStatus callback.
func StatusPatcher(mutate func(*Status)) func(current json.RawMessage) (json.RawMessage, error) {
	return func(current json.RawMessage) (json.RawMessage, error) {
		var s Status
		if err := UnmarshalStatusFromStorage(current, &s); err != nil {
			return nil, err
		}
		mutate(&s)
		return MarshalStatusForStorage(s)
	}
}

// UnmarshalStatusFromStorage is the read-side inverse of
// MarshalStatusForStorage: decode a status JSONB payload back into a
// live Status struct, including the internal-only ObservedGeneration fields.
func UnmarshalStatusFromStorage(data []byte, s *Status) error {
	if len(data) == 0 {
		*s = Status{}
		return nil
	}
	var w statusStore
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	conds := make([]Condition, len(w.Conditions))
	for i, c := range w.Conditions {
		conds[i] = Condition(c)
	}
	*s = Status{
		ObservedGeneration: w.ObservedGeneration,
		Conditions:         conds,
		Details:            w.Details,
	}
	return nil
}

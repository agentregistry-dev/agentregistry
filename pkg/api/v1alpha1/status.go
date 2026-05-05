package v1alpha1

import (
	"encoding/json"
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
type Condition struct {
	Type               string          `json:"type" yaml:"type"`
	Status             ConditionStatus `json:"status" yaml:"status"`
	Reason             string          `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string          `json:"message,omitempty" yaml:"message,omitempty"`
	LastTransitionTime time.Time       `json:"lastTransitionTime,omitzero" yaml:"lastTransitionTime,omitempty"`
}

// Status is the observed-state subresource. Version is the
// system-assigned monotonic integer identifying the immutable resource
// row this status belongs to; Conditions is the list of fine-grained
// state facets written by the reconciler and service layer. No Phase
// roll-up — K8s deprecated it in favor of Conditions, and carrying a
// string summary encourages downstream string-comparison anti-patterns.
type Status struct {
	Version    int         `json:"version,omitempty" yaml:"version,omitempty"`
	Conditions []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
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

// conditionStore is the on-disk shape of a Condition. Currently identical
// to Condition; retained as a separate type so storage-only fields can be
// added without leaking into the wire schema.
type conditionStore struct {
	Type               string          `json:"type"`
	Status             ConditionStatus `json:"status"`
	Reason             string          `json:"reason,omitempty"`
	Message            string          `json:"message,omitempty"`
	LastTransitionTime time.Time       `json:"lastTransitionTime,omitzero"`
}

// statusStore is the on-disk shape of Status. Mirrors Status today; kept
// distinct so storage-only fields can be added without altering the wire
// schema. See MarshalStatusForStorage / UnmarshalStatusFromStorage.
type statusStore struct {
	Version    int              `json:"version,omitempty"`
	Conditions []conditionStore `json:"conditions,omitempty"`
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
		Version:    s.Version,
		Conditions: storeConds,
	})
}

// StatusPatcher adapts a typed Status mutator into the opaque-bytes
// signature that v1alpha1store.PatchOpts.Status / Store.PatchStatus
// expect. Callers that use the typed v1alpha1.Status schema wrap their
// SetCondition logic here:
//
//	store.PatchStatus(ctx, ns, name, version, v1alpha1.StatusPatcher(
//	    func(s *v1alpha1.Status) {
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

// SetStatusVersionBytes sets Status.Version on a marshaled status payload,
// preserving any other status fields. Returns the new payload bytes.
// Used by the store on read to ensure status.version mirrors the row's
// version column even if the on-disk status drifted, and by the apply
// pipeline to stamp the system-assigned version onto the response body.
func SetStatusVersionBytes(data []byte, v int) ([]byte, error) {
	var s Status
	if err := UnmarshalStatusFromStorage(data, &s); err != nil {
		return nil, err
	}
	s.Version = v
	return MarshalStatusForStorage(s)
}

// UnmarshalStatusFromStorage is the read-side inverse of
// MarshalStatusForStorage: decode a status JSONB payload back into a
// live Status struct.
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
		Version:    w.Version,
		Conditions: conds,
	}
	return nil
}

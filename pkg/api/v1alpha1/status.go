package v1alpha1

import "time"

// ConditionStatus values, matching Kubernetes apimachinery/pkg/apis/meta/v1.
type ConditionStatus string

const (
	ConditionTrue    ConditionStatus = "True"
	ConditionFalse   ConditionStatus = "False"
	ConditionUnknown ConditionStatus = "Unknown"
)

// Condition describes one facet of a resource's observed state. Modeled after
// the Kubernetes v1.Condition: Type is the named condition (e.g. "Ready",
// "Validated", "Published"); Status is True/False/Unknown; Reason is a
// machine-readable CamelCase token; Message is a human-readable explanation;
// LastTransitionTime is when Status last flipped; ObservedGeneration is the
// spec generation this condition was derived from.
type Condition struct {
	Type               string          `json:"type" yaml:"type"`
	Status             ConditionStatus `json:"status" yaml:"status"`
	Reason             string          `json:"reason,omitempty" yaml:"reason,omitempty"`
	Message            string          `json:"message,omitempty" yaml:"message,omitempty"`
	LastTransitionTime time.Time       `json:"lastTransitionTime,omitzero" yaml:"lastTransitionTime,omitempty"`
	ObservedGeneration int64           `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
}

// Status is the observed-state subresource. ObservedGeneration is the top-level
// roll-up of the highest metadata.generation any reconciler has acted on; Phase
// is an optional short status summary; Conditions is the list of fine-grained
// state facets written by the reconciler and service layer.
type Status struct {
	ObservedGeneration int64       `json:"observedGeneration,omitempty" yaml:"observedGeneration,omitempty"`
	Phase              string      `json:"phase,omitempty" yaml:"phase,omitempty"`
	Conditions         []Condition `json:"conditions,omitempty" yaml:"conditions,omitempty"`
}

// SetCondition adds or updates the condition matching c.Type on s. If an entry
// exists and its Status matches c.Status, the existing LastTransitionTime is
// preserved; otherwise LastTransitionTime is set to now (or c.LastTransitionTime
// if non-zero). Reason, Message, and ObservedGeneration are always overwritten.
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

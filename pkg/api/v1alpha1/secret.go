package v1alpha1

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
)

// SecretType identifies the payload schema, independently of its storage backend.
type SecretType string

const SecretTypeOpaque SecretType = "Opaque"

// Secret is a write-only credential resource. Data and StringData are accepted
// on apply but must be removed by a Prepare hook before metadata persistence.
type Secret struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta   `json:"metadata" yaml:"metadata"`
	Spec     SecretSpec   `json:"spec" yaml:"spec"`
	Status   SecretStatus `json:"status,omitzero" yaml:"status,omitempty"`
}

// SecretSpec is the desired metadata and write-only payload.
type SecretSpec struct {
	Type       SecretType        `json:"type,omitempty" yaml:"type,omitempty"`
	Immutable  bool              `json:"immutable,omitempty" yaml:"immutable,omitempty"`
	Data       map[string]string `json:"data,omitempty" yaml:"data,omitempty"`
	StringData map[string]string `json:"stringData,omitempty" yaml:"stringData,omitempty"`
}

// SecretStatus exposes payload keys without exposing their values.
type SecretStatus struct {
	DataKeys []string `json:"dataKeys,omitempty" yaml:"dataKeys,omitempty"`
}

// SecretRef identifies a Secret and optionally one payload key.
type SecretRef struct {
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key,omitempty" yaml:"key,omitempty"`
}

var secretNameRegexp = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func init() {
	MustRegisterKind[*Secret, SecretSpec](KindSecret, WithMutableObjectStorage())
}

func (s *Secret) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(s.Metadata)...)
	if !secretNameRegexp.MatchString(s.Metadata.Name) {
		errs.Append("metadata.name", fmt.Errorf("%w: must match %s", ErrInvalidFormat, secretNameRegexp.String()))
	}
	if s.Spec.Type != "" && s.Spec.Type != SecretTypeOpaque {
		errs.Append("spec.type", fmt.Errorf("%w: only %q is supported", ErrInvalidFormat, SecretTypeOpaque))
	}
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// MergedData decodes Data and overlays raw StringData values.
func (s *Secret) MergedData() (map[string][]byte, error) {
	out := make(map[string][]byte, len(s.Spec.Data)+len(s.Spec.StringData))
	for key, value := range s.Spec.Data {
		decoded, err := base64.StdEncoding.DecodeString(value)
		if err != nil {
			return nil, fmt.Errorf("data[%q]: %w", key, err)
		}
		out[key] = decoded
	}
	for key, value := range s.Spec.StringData {
		out[key] = []byte(value)
	}
	return out, nil
}

// StripValues removes payloads and records their sorted keys.
func (s *Secret) StripValues(data map[string][]byte) {
	s.Spec.Data = nil
	s.Spec.StringData = nil
	keys := make([]string, 0, len(data))
	for key := range data {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	s.Status.DataKeys = keys
}

func (s *Secret) MarshalSpec() (json.RawMessage, error) { return json.Marshal(s.Spec) }

func (s *Secret) UnmarshalSpec(data json.RawMessage) error { return json.Unmarshal(data, &s.Spec) }

func (s *Secret) MarshalStatus() (json.RawMessage, error) {
	if len(s.Status.DataKeys) == 0 {
		return nil, nil
	}
	return json.Marshal(s.Status)
}

func (s *Secret) UnmarshalStatus(data json.RawMessage) error {
	if len(data) == 0 || string(data) == "null" {
		s.Status = SecretStatus{}
		return nil
	}
	return json.Unmarshal(data, &s.Status)
}

func (s *Secret) GetMetadata() *ObjectMeta { return &s.Metadata }

func (s *Secret) SetMetadata(metadata ObjectMeta) { s.Metadata = metadata }

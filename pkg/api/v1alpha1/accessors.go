package v1alpha1

import (
	"context"
	"encoding/json"
)

func (tm *TypeMeta) GetAPIVersion() string { return tm.APIVersion }
func (tm *TypeMeta) GetKind() string       { return tm.Kind }
func (tm *TypeMeta) SetTypeMeta(t TypeMeta) {
	*tm = t
}

func (m *ObjectMeta) GetMetadata() *ObjectMeta { return m }
func (m *ObjectMeta) SetMetadata(meta ObjectMeta) {
	*m = meta
}

// Object is the minimal interface satisfied by every typed v1alpha1 envelope
// (Agent, MCPServer, Skill, Prompt, Provider, Deployment; enterprise kinds
// opt in too). It lets generic code operate on any resource without
// reflection.
//
// Status is intentionally exchanged as json.RawMessage on this interface.
// The envelope itself stays agnostic to per-kind status schemas:
//   - OSS kinds currently bind Status to the typed v1alpha1.Status
//     (observed-generation + K8s-style Conditions) via the accessor
//     methods below.
//   - Enterprise kinds can use any shape they like without conforming to
//     meta.v1 conditions.
//
// MarshalStatus / UnmarshalStatus are the codec hooks the generic Store and
// handlers use to read/write status from the status JSONB column.
type Object interface {
	GetAPIVersion() string
	GetKind() string
	SetTypeMeta(TypeMeta)
	GetMetadata() *ObjectMeta
	SetMetadata(ObjectMeta)
	// MarshalSpec returns the JSON encoding of this object's Spec field.
	MarshalSpec() (json.RawMessage, error)
	// UnmarshalSpec decodes the given JSON bytes into this object's Spec field.
	UnmarshalSpec(data json.RawMessage) error
	// MarshalStatus returns the JSON encoding of this object's Status field.
	// Empty-status objects return `nil, nil`.
	MarshalStatus() (json.RawMessage, error)
	// UnmarshalStatus decodes the given JSON bytes into this object's Status
	// field. Empty/nil input resets the status to its zero value.
	UnmarshalStatus(data json.RawMessage) error
}

// StructuralValidator runs zero-I/O validation on an envelope.
type StructuralValidator interface {
	Validate() error
}

// MetadataVersionDefaulter is an optional capability for kinds where
// metadata.version carries no semantic meaning — Provider (a
// connection handle to one execution target) and Deployment (a
// runtime binding). The shared apply pipeline calls
// DefaultMetadataVersion when the request body's metadata.version is
// empty, so YAML manifests for these kinds don't have to carry a
// fabricated placeholder version. Other kinds — Agent, MCPServer,
// Skill, Prompt — don't implement this interface; their version is
// real and required.
//
// Returning a non-empty constant ("1" by convention) is what gets
// stored in the (namespace, name, version) PK. Returning "" defers
// to the standard "version required" validator.
//
// Pair with ValidateObjectMetaUnversioned in the kind's Validate.
type MetadataVersionDefaulter interface {
	DefaultMetadataVersion() string
}

// RefResolver validates cross-resource references for an envelope.
type RefResolver interface {
	ResolveRefs(ctx context.Context, resolver ResolverFunc) error
}

// RegistryValidatable validates packages against external registry metadata.
type RegistryValidatable interface {
	ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error
}

// ValidateObject runs structural validation when obj opts into it.
func ValidateObject(obj Object) error {
	if v, ok := any(obj).(StructuralValidator); ok {
		return v.Validate()
	}
	return nil
}

// ResolveObjectRefs validates cross-resource refs when obj carries them.
func ResolveObjectRefs(ctx context.Context, obj Object, resolver ResolverFunc) error {
	if resolver == nil {
		return nil
	}
	if v, ok := any(obj).(RefResolver); ok {
		return v.ResolveRefs(ctx, resolver)
	}
	return nil
}

// ValidateObjectRegistries validates package registries when obj exposes them.
func ValidateObjectRegistries(ctx context.Context, obj Object, v RegistryValidatorFunc) error {
	if v == nil {
		return nil
	}
	if rv, ok := any(obj).(RegistryValidatable); ok {
		return rv.ValidateRegistries(ctx, v)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Per-kind accessors. Spec codec routes through each kind's typed Spec; Status
// codec routes through the typed v1alpha1.Status (OSS default — opt in by
// typing the Status field as v1alpha1.Status). Kinds that want their own
// status shape override MarshalStatus / UnmarshalStatus with the appropriate
// json.Marshal / json.Unmarshal on their typed field.
// -----------------------------------------------------------------------------

func (a *Agent) GetMetadata() *ObjectMeta { return &a.Metadata }
func (a *Agent) SetMetadata(meta ObjectMeta) {
	a.Metadata = meta
}
func (a *Agent) MarshalSpec() (json.RawMessage, error) { return json.Marshal(a.Spec) }
func (a *Agent) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &a.Spec)
}
func (a *Agent) MarshalStatus() (json.RawMessage, error) { return MarshalStatusForStorage(a.Status) }
func (a *Agent) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &a.Status)
}

func (m *MCPServer) GetMetadata() *ObjectMeta { return &m.Metadata }
func (m *MCPServer) SetMetadata(meta ObjectMeta) {
	m.Metadata = meta
}
func (m *MCPServer) MarshalSpec() (json.RawMessage, error) { return json.Marshal(m.Spec) }
func (m *MCPServer) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &m.Spec)
}
func (m *MCPServer) MarshalStatus() (json.RawMessage, error) {
	return MarshalStatusForStorage(m.Status)
}
func (m *MCPServer) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &m.Status)
}

func (s *Skill) GetMetadata() *ObjectMeta { return &s.Metadata }
func (s *Skill) SetMetadata(meta ObjectMeta) {
	s.Metadata = meta
}
func (s *Skill) MarshalSpec() (json.RawMessage, error) { return json.Marshal(s.Spec) }
func (s *Skill) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &s.Spec)
}
func (s *Skill) MarshalStatus() (json.RawMessage, error) { return MarshalStatusForStorage(s.Status) }
func (s *Skill) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &s.Status)
}

func (p *Prompt) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Prompt) SetMetadata(meta ObjectMeta) {
	p.Metadata = meta
}
func (p *Prompt) MarshalSpec() (json.RawMessage, error) { return json.Marshal(p.Spec) }
func (p *Prompt) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &p.Spec)
}
func (p *Prompt) MarshalStatus() (json.RawMessage, error) { return MarshalStatusForStorage(p.Status) }
func (p *Prompt) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &p.Status)
}

func (p *Provider) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Provider) SetMetadata(meta ObjectMeta) {
	p.Metadata = meta
}
func (p *Provider) MarshalSpec() (json.RawMessage, error) { return json.Marshal(p.Spec) }
func (p *Provider) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &p.Spec)
}
func (p *Provider) MarshalStatus() (json.RawMessage, error) {
	return MarshalStatusForStorage(p.Status)
}
func (p *Provider) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &p.Status)
}

func (d *Deployment) GetMetadata() *ObjectMeta { return &d.Metadata }
func (d *Deployment) SetMetadata(meta ObjectMeta) {
	d.Metadata = meta
}
func (d *Deployment) MarshalSpec() (json.RawMessage, error) { return json.Marshal(d.Spec) }
func (d *Deployment) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &d.Spec)
}
func (d *Deployment) MarshalStatus() (json.RawMessage, error) {
	return MarshalStatusForStorage(d.Status)
}
func (d *Deployment) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &d.Status)
}

func (r *RemoteMCPServer) GetMetadata() *ObjectMeta { return &r.Metadata }
func (r *RemoteMCPServer) SetMetadata(meta ObjectMeta) {
	r.Metadata = meta
}
func (r *RemoteMCPServer) MarshalSpec() (json.RawMessage, error) { return json.Marshal(r.Spec) }
func (r *RemoteMCPServer) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &r.Spec)
}
func (r *RemoteMCPServer) MarshalStatus() (json.RawMessage, error) {
	return MarshalStatusForStorage(r.Status)
}
func (r *RemoteMCPServer) UnmarshalStatus(data json.RawMessage) error {
	return UnmarshalStatusFromStorage(data, &r.Status)
}

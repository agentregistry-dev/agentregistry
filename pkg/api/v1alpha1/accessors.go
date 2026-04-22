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

func (s *Status) GetStatus() *Status { return s }
func (s *Status) SetStatus(status Status) {
	*s = status
}

func (a *Agent) GetMetadata() *ObjectMeta { return &a.Metadata }
func (a *Agent) SetMetadata(meta ObjectMeta) {
	a.Metadata = meta
}
func (a *Agent) GetStatus() *Status { return &a.Status }
func (a *Agent) SetStatus(status Status) {
	a.Status = status
}

func (m *MCPServer) GetMetadata() *ObjectMeta { return &m.Metadata }
func (m *MCPServer) SetMetadata(meta ObjectMeta) {
	m.Metadata = meta
}
func (m *MCPServer) GetStatus() *Status { return &m.Status }
func (m *MCPServer) SetStatus(status Status) {
	m.Status = status
}

func (s *Skill) GetMetadata() *ObjectMeta { return &s.Metadata }
func (s *Skill) SetMetadata(meta ObjectMeta) {
	s.Metadata = meta
}
func (s *Skill) GetStatus() *Status { return &s.Status }
func (s *Skill) SetStatus(status Status) {
	s.Status = status
}

func (p *Prompt) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Prompt) SetMetadata(meta ObjectMeta) {
	p.Metadata = meta
}
func (p *Prompt) GetStatus() *Status { return &p.Status }
func (p *Prompt) SetStatus(status Status) {
	p.Status = status
}

func (p *Provider) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Provider) SetMetadata(meta ObjectMeta) {
	p.Metadata = meta
}
func (p *Provider) GetStatus() *Status { return &p.Status }
func (p *Provider) SetStatus(status Status) {
	p.Status = status
}

func (d *Deployment) GetMetadata() *ObjectMeta { return &d.Metadata }
func (d *Deployment) SetMetadata(meta ObjectMeta) {
	d.Metadata = meta
}
func (d *Deployment) GetStatus() *Status { return &d.Status }
func (d *Deployment) SetStatus(status Status) {
	d.Status = status
}

// Object is the minimal interface satisfied by every typed v1alpha1 envelope
// (Agent, MCPServer, Skill, Prompt, Provider, Deployment). It lets generic
// code operate on any resource without reflection.
type Object interface {
	GetAPIVersion() string
	GetKind() string
	SetTypeMeta(TypeMeta)
	GetMetadata() *ObjectMeta
	SetMetadata(ObjectMeta)
	GetStatus() *Status
	SetStatus(Status)
	// MarshalSpec returns the JSON encoding of this object's Spec field.
	MarshalSpec() (json.RawMessage, error)
	// UnmarshalSpec decodes the given JSON bytes into this object's Spec field.
	UnmarshalSpec(data json.RawMessage) error
}

// StructuralValidator runs zero-I/O validation on an envelope.
type StructuralValidator interface {
	Validate() error
}

// RefResolver validates cross-resource references for an envelope.
type RefResolver interface {
	ResolveRefs(ctx context.Context, resolver ResolverFunc) error
}

// RegistryValidatable validates packages against external registry metadata.
type RegistryValidatable interface {
	ValidateRegistries(ctx context.Context, v RegistryValidatorFunc) error
}

// UniqueRemoteURLsValidatable checks the cross-row remote URL invariant.
type UniqueRemoteURLsValidatable interface {
	ValidateUniqueRemoteURLs(ctx context.Context, check UniqueRemoteURLsFunc) error
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

// ValidateObjectRemoteURLs validates remote URL uniqueness when obj exposes
// remote endpoints.
func ValidateObjectRemoteURLs(ctx context.Context, obj Object, check UniqueRemoteURLsFunc) error {
	if check == nil {
		return nil
	}
	if rv, ok := any(obj).(UniqueRemoteURLsValidatable); ok {
		return rv.ValidateUniqueRemoteURLs(ctx, check)
	}
	return nil
}

// Spec marshal/unmarshal methods. json.Marshal/Unmarshal on a typed Spec
// round-trips cleanly; these are the shim for generic handlers that deal
// in json.RawMessage across kinds.

func (a *Agent) MarshalSpec() (json.RawMessage, error) { return json.Marshal(a.Spec) }
func (a *Agent) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &a.Spec)
}

func (m *MCPServer) MarshalSpec() (json.RawMessage, error) { return json.Marshal(m.Spec) }
func (m *MCPServer) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &m.Spec)
}

func (s *Skill) MarshalSpec() (json.RawMessage, error) { return json.Marshal(s.Spec) }
func (s *Skill) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &s.Spec)
}

func (p *Prompt) MarshalSpec() (json.RawMessage, error) { return json.Marshal(p.Spec) }
func (p *Prompt) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &p.Spec)
}

func (p *Provider) MarshalSpec() (json.RawMessage, error) { return json.Marshal(p.Spec) }
func (p *Provider) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &p.Spec)
}

func (d *Deployment) MarshalSpec() (json.RawMessage, error) { return json.Marshal(d.Spec) }
func (d *Deployment) UnmarshalSpec(data json.RawMessage) error {
	return json.Unmarshal(data, &d.Spec)
}

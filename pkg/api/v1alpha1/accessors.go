package v1alpha1

import "encoding/json"

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

// Pointer receivers so SetMetadata/SetStatus mutate the caller's value.
// Trivially mechanical across all six kinds.

func (a *Agent) GetAPIVersion() string    { return a.APIVersion }
func (a *Agent) GetKind() string          { return a.Kind }
func (a *Agent) SetTypeMeta(t TypeMeta)   { a.TypeMeta = t }
func (a *Agent) GetMetadata() *ObjectMeta { return &a.Metadata }
func (a *Agent) SetMetadata(m ObjectMeta) { a.Metadata = m }
func (a *Agent) GetStatus() *Status       { return &a.Status }
func (a *Agent) SetStatus(s Status)       { a.Status = s }

func (m *MCPServer) GetAPIVersion() string       { return m.APIVersion }
func (m *MCPServer) GetKind() string             { return m.Kind }
func (m *MCPServer) SetTypeMeta(t TypeMeta)      { m.TypeMeta = t }
func (m *MCPServer) GetMetadata() *ObjectMeta    { return &m.Metadata }
func (m *MCPServer) SetMetadata(meta ObjectMeta) { m.Metadata = meta }
func (m *MCPServer) GetStatus() *Status          { return &m.Status }
func (m *MCPServer) SetStatus(s Status)          { m.Status = s }

func (s *Skill) GetAPIVersion() string    { return s.APIVersion }
func (s *Skill) GetKind() string          { return s.Kind }
func (s *Skill) SetTypeMeta(t TypeMeta)   { s.TypeMeta = t }
func (s *Skill) GetMetadata() *ObjectMeta { return &s.Metadata }
func (s *Skill) SetMetadata(m ObjectMeta) { s.Metadata = m }
func (s *Skill) GetStatus() *Status       { return &s.Status }
func (s *Skill) SetStatus(st Status)      { s.Status = st }

func (p *Prompt) GetAPIVersion() string    { return p.APIVersion }
func (p *Prompt) GetKind() string          { return p.Kind }
func (p *Prompt) SetTypeMeta(t TypeMeta)   { p.TypeMeta = t }
func (p *Prompt) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Prompt) SetMetadata(m ObjectMeta) { p.Metadata = m }
func (p *Prompt) GetStatus() *Status       { return &p.Status }
func (p *Prompt) SetStatus(s Status)       { p.Status = s }

func (p *Provider) GetAPIVersion() string    { return p.APIVersion }
func (p *Provider) GetKind() string          { return p.Kind }
func (p *Provider) SetTypeMeta(t TypeMeta)   { p.TypeMeta = t }
func (p *Provider) GetMetadata() *ObjectMeta { return &p.Metadata }
func (p *Provider) SetMetadata(m ObjectMeta) { p.Metadata = m }
func (p *Provider) GetStatus() *Status       { return &p.Status }
func (p *Provider) SetStatus(s Status)       { p.Status = s }

func (d *Deployment) GetAPIVersion() string    { return d.APIVersion }
func (d *Deployment) GetKind() string          { return d.Kind }
func (d *Deployment) SetTypeMeta(t TypeMeta)   { d.TypeMeta = t }
func (d *Deployment) GetMetadata() *ObjectMeta { return &d.Metadata }
func (d *Deployment) SetMetadata(m ObjectMeta) { d.Metadata = m }
func (d *Deployment) GetStatus() *Status       { return &d.Status }
func (d *Deployment) SetStatus(s Status)       { d.Status = s }

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

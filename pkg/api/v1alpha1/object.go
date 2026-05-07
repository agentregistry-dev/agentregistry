package v1alpha1

import (
	"encoding/json"
	"time"
)

// TypeMeta carries apiVersion + kind. Every typed object embeds this inline so
// that marshaled output matches the Kubernetes-style envelope.
type TypeMeta struct {
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`
	Kind       string `json:"kind" yaml:"kind"`
}

// DefaultNamespace is the namespace used when a caller doesn't supply one
// (blank metadata.namespace in a YAML apply, for instance). Servers fill
// missing namespaces with this value so identity is always fully qualified
// at rest. The string matches the Kubernetes convention for consistency.
const DefaultNamespace = "default"

// ObjectMeta is the metadata block common to every resource.
//
// Namespace, Name, Labels, Annotations, and Tag are user-settable for
// content-registry kinds. CreatedAt, UpdatedAt, and DeletionTimestamp are
// server-managed. Version remains only for legacy infra/config kinds whose
// storage still uses the old mutable (namespace, name, version) tuple.
//
// Content-registry identity is (Namespace, Name, Tag). Users may supply
// metadata.tag to pin manifests declaratively before applying anything to a
// live server. When Tag is omitted, the store fills it with the literal
// "latest" tag.
//
// Legacy identity at the database level is (Namespace, Name, Version).
// Namespace is an internal detail today — it defaults to "default" on
// apply and is stripped from responses when it equals "default" so the
// multi-tenant surface stays hidden until we deliberately enable it.
//
// Labels vs Annotations (Kubernetes convention):
//   - Labels are queryable: short key/value pairs, GIN-indexed, used for
//     filtering + selection. Enforce the K8s label format.
//   - Annotations are narrative: arbitrary key/value pairs for controller
//     state, tool metadata, etc. Not indexed; can carry larger payloads.
//     Callers read annotations by key; the server never filters on them.
//
// DeletionTimestamp marks a row as terminating. Soft-delete is
// server-side: a DELETE call sets DeletionTimestamp and the row is
// later hard-deleted by the GC pass. There is no user-facing finalizer
// API; the storage layer retains a `finalizers` column for a future
// orphan-reconciler hook, but normal apply / delete flow does not
// populate or drain it.
type ObjectMeta struct {
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name        string            `json:"name" yaml:"name"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`

	// Tag is the user-visible identity for content-registry kinds
	// (Agent, MCPServer, RemoteMCPServer, Skill, Prompt).
	Tag string `json:"tag,omitempty" yaml:"tag,omitempty"`

	// Version is the legacy Provider/Deployment Store's string identity.
	// Content-registry manifests must not include metadata.version.
	Version string `json:"version,omitempty" yaml:"version,omitempty"`

	CreatedAt time.Time `json:"createdAt,omitzero" yaml:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitzero" yaml:"updatedAt,omitempty"`

	// DeletionTimestamp is set by the Store when Delete is called. A non-nil
	// DeletionTimestamp means the object is terminating; the row stays
	// observable via Get until the GC pass purges it. Clients MUST NOT
	// set this on apply.
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty" yaml:"deletionTimestamp,omitempty"`
}

// objectMetaWire is the marshaling shape used by ObjectMeta.MarshalJSON.
// Aliased so json.Marshal on the alias doesn't recurse into our custom
// method.
type objectMetaWire ObjectMeta

// MarshalJSON strips Namespace when it equals DefaultNamespace so
// responses don't leak the namespace surface while it remains hidden
// from the user-facing API. Internal storage always carries the full
// identity (ns, name, version); this only affects wire rendering.
//
// Inbound defaulting (empty → "default") happens at the apply /
// import pipeline boundary (see resource.prepareApplyDoc and
// importer.Options.Namespace), not on UnmarshalJSON — callers like
// the importer need to keep the empty-namespace signal around so they
// can layer their own default on top.
func (m ObjectMeta) MarshalJSON() ([]byte, error) {
	w := objectMetaWire(m)
	if w.Namespace == DefaultNamespace {
		w.Namespace = ""
	}
	return json.Marshal(w)
}

// NamespaceOrDefault returns m.Namespace, or DefaultNamespace when the
// field is empty. Use when building display strings / ids that should
// read "default/<name>/<version>" even though the wire has elided the
// namespace.
func (m ObjectMeta) NamespaceOrDefault() string {
	if m.Namespace == "" {
		return DefaultNamespace
	}
	return m.Namespace
}

// RawObject is the generic wire envelope used during decode and apply dispatch
// when the concrete Kind is not yet known. Spec AND Status are both held as
// raw JSON bytes so the envelope layer stays agnostic to per-kind schemas:
// OSS kinds layer a typed v1alpha1.Status (K8s-style conditions) on top;
// enterprise kinds can ship any JSON shape they like
// without having to conform to meta.v1 conditions.
//
// Callers route into a typed object via Scheme.Decode / Scheme.DecodeMulti
// (or EnvelopeFromRaw); each kind's UnmarshalStatus is the inverse of the
// per-kind MarshalStatus and decides how to decode the bytes.
type RawObject struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta      `json:"metadata" yaml:"metadata"`
	Spec     json.RawMessage `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status   json.RawMessage `json:"status,omitempty" yaml:"status,omitempty"`
}

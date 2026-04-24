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
// Namespace, Name, Version, Labels, and Annotations are user-settable.
// CreatedAt, UpdatedAt, and DeletionTimestamp are server-managed: the API
// ignores them on apply and overwrites them on response.
//
// Generation and Finalizers are internal coordination primitives —
// Generation drives reconciler convergence (paired with
// Status.ObservedGeneration); Finalizers stage safe teardown for
// reconcilers that need to clean up external state before a row is GC'd.
// Both are populated from the database row and used by internal Go code
// (coordinators, adapters, store helpers) but are NOT emitted on the
// wire: the JSON tag is `-`, so OpenAPI schemas don't reveal them and
// clients can't set them on apply. If we expose these to users in a
// future release we'll relax the tags.
//
// (Namespace, Name, Version) together form the identity of a resource;
// that triple is the composite primary key at the database level.
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
// DeletionTimestamp marks a row as terminating. The soft-delete +
// finalizer mechanism runs entirely server-side; callers only observe
// the terminating state via DeletionTimestamp.
type ObjectMeta struct {
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version,omitempty" yaml:"version,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty" yaml:"annotations,omitempty"`

	// Generation is server-managed and internal. Populated from the DB row
	// for internal Go consumers (coordinators, status reconcilers); hidden
	// from the wire.
	Generation int64     `json:"-" yaml:"-"`
	CreatedAt  time.Time `json:"createdAt,omitzero" yaml:"createdAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitzero" yaml:"updatedAt,omitempty"`

	// DeletionTimestamp is set by the Store when Delete is called. A non-nil
	// DeletionTimestamp means the object is terminating; callers may still
	// observe it until the finalizer list drains server-side, then the row
	// is hard-deleted by GC. Clients MUST NOT set this on apply.
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty" yaml:"deletionTimestamp,omitempty"`

	// Finalizers are internal coordination tokens owned by reconcilers.
	// Kept in the struct so internal code (coordinator, adapters) can read
	// and mutate them, but hidden from the wire — callers can't set
	// finalizers on apply and never see them in responses.
	Finalizers []string `json:"-" yaml:"-"`
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
// when the concrete Kind is not yet known. Spec is held as raw JSON bytes;
// callers route into a typed object via Scheme.Decode / Scheme.DecodeMulti
// or manually unmarshal Spec into a typed value.
type RawObject struct {
	TypeMeta `json:",inline" yaml:",inline"`
	Metadata ObjectMeta      `json:"metadata" yaml:"metadata"`
	Spec     json.RawMessage `json:"spec,omitempty" yaml:"spec,omitempty"`
	Status   Status          `json:"status,omitzero" yaml:"status,omitempty"`
}

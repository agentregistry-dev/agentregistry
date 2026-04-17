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
// Namespace, Name, Version, and Labels are user-set. Generation, CreatedAt,
// UpdatedAt, DeletionTimestamp, and Finalizers are server-managed: the API
// ignores them on apply and overwrites them on response. They are exposed in
// the wire format so clients can observe reconciliation convergence by
// comparing metadata.generation against status.observedGeneration — the
// same reason Kubernetes surfaces generation.
//
// (Namespace, Name, Version) together form the identity of a resource; that
// triple is the composite primary key at the database level. Namespace
// scopes the object, Name identifies it within the namespace, Version is
// user-set (semver-like or any opaque string). Generation increments on
// spec mutation only — no-op reapplies preserve generation.
//
// DeletionTimestamp + Finalizers implement Kubernetes-style soft delete:
// Delete sets DeletionTimestamp and leaves the row in place until every
// finalizer has been removed by its owner. A GC pass hard-deletes rows
// where DeletionTimestamp != nil AND Finalizers is empty.
type ObjectMeta struct {
	Namespace string            `json:"namespace" yaml:"namespace"`
	Name      string            `json:"name" yaml:"name"`
	Version   string            `json:"version,omitempty" yaml:"version,omitempty"`
	Labels    map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Generation is server-managed. Clients MUST NOT set it on apply; the
	// Store overwrites on every Upsert.
	Generation int64     `json:"generation,omitempty" yaml:"generation,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitzero" yaml:"createdAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitzero" yaml:"updatedAt,omitempty"`

	// DeletionTimestamp is set by the Store when Delete is called. A non-nil
	// DeletionTimestamp means the object is terminating; callers may still
	// observe it until Finalizers drains, then it is hard-deleted by GC.
	// Clients MUST NOT set this on apply.
	DeletionTimestamp *time.Time `json:"deletionTimestamp,omitempty" yaml:"deletionTimestamp,omitempty"`

	// Finalizers are string tokens owned by reconcilers/controllers; each
	// owner removes its token when its cleanup for this object has finished.
	// The GC pass requires this slice to be empty before hard-deleting a
	// terminating row. Clients may add/remove finalizers on apply; the Store
	// preserves them across spec updates.
	Finalizers []string `json:"finalizers,omitempty" yaml:"finalizers,omitempty"`
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

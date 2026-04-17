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

// ObjectMeta is the metadata block common to every resource.
//
// Name, Version, and Labels are user-set. Generation, CreatedAt, and UpdatedAt
// are server-managed: the API ignores them on apply and overwrites them on
// response. They are exposed in the wire format so that clients can observe
// reconciliation convergence by comparing metadata.generation against
// status.observedGeneration — the same reason Kubernetes surfaces generation.
//
// Name and Version together form the identity of a resource; (Name, Version)
// is the composite primary key at the database level. Version is user-set
// (semver-like or any opaque string). Generation increments on spec mutation
// only — no-op reapplies preserve generation.
type ObjectMeta struct {
	Name    string            `json:"name" yaml:"name"`
	Version string            `json:"version,omitempty" yaml:"version,omitempty"`
	Labels  map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// Generation is server-managed. Clients MUST NOT set it on apply; it is
	// overwritten by the Store on every Upsert.
	Generation int64     `json:"generation,omitempty" yaml:"generation,omitempty"`
	CreatedAt  time.Time `json:"createdAt,omitzero" yaml:"createdAt,omitempty"`
	UpdatedAt  time.Time `json:"updatedAt,omitzero" yaml:"updatedAt,omitempty"`
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

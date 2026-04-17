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
// Name and Version together form the identity of a resource; (Name, Version)
// is the composite primary key at the database level. Version is user-set
// (semver-like or any opaque string). Generation is server-assigned and
// increments on spec mutation — it is the counter that Status.ObservedGeneration
// tracks against.
type ObjectMeta struct {
	Name       string            `json:"name" yaml:"name"`
	Version    string            `json:"version,omitempty" yaml:"version,omitempty"`
	Labels     map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	Generation int64             `json:"generation,omitempty" yaml:"generation,omitempty"`
	CreatedAt  time.Time         `json:"createdAt,omitzero" yaml:"createdAt,omitempty"`
	UpdatedAt  time.Time         `json:"updatedAt,omitzero" yaml:"updatedAt,omitempty"`
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

// Package kinds defines the registry of resource types (agent, skill, prompt,
// mcp, provider, deployment, and enterprise-contributed kinds). The registry
// is the single source of truth for every layer that needs per-kind behavior:
// the server dispatcher for POST /v0/apply, the CLI for arctl apply/delete/get/init/build,
// init templates, and table rendering.
package kinds

import (
	"context"
	"io"
	"reflect"
)

// Metadata is the name/version envelope shared by every document.
type Metadata struct {
	Name    string            `yaml:"name" json:"name"`
	Version string            `yaml:"version,omitempty" json:"version,omitempty"`
	Labels  map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
}

// Document is a single apply/delete unit.
// After Decode, Spec holds a concrete typed pointer (e.g. *AgentSpec).
type Document struct {
	APIVersion string   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string   `yaml:"kind" json:"kind"`
	Metadata   Metadata `yaml:"metadata" json:"metadata"`
	Spec       any      `yaml:"spec" json:"spec"`
}

// Status is the per-document outcome of an apply call.
type Status string

// Per-document outcome values.
const (
	StatusCreated    Status = "created"    // dry-run: resource doesn't exist, would be created
	StatusConfigured Status = "configured" // dry-run: resource exists with different spec, would update
	StatusUnchanged  Status = "unchanged"  // dry-run: resource exists with same spec, no-op
	StatusApplied    Status = "applied"    // non-dry-run: resource was actually mutated
	StatusDeleted    Status = "deleted"    // resource was deleted
	StatusFailed     Status = "failed"     // error
)

// Result is returned per document by the batch apply handler.
type Result struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Status  Status `json:"status"`
	Error   string `json:"error,omitempty"`
}

// ApplyOpts carries cross-cutting options from the batch endpoint to per-kind Apply fns.
type ApplyOpts struct {
	Force  bool
	DryRun bool
}

// Column describes a single column for `arctl get` table rendering.
type Column struct {
	Header string
}

// InitParams is the user-supplied input for `arctl init <kind>`.
type InitParams struct {
	Name      string
	Version   string
	ExtraFlag map[string]string
}

// BuildParams holds build-time inputs for `arctl build`.
type BuildParams struct {
	WorkDir  string
	ImageTag string
}

// ApplyFunc applies a single document. Implementations must be safe for concurrent use.
type ApplyFunc func(ctx context.Context, doc *Document, opts ApplyOpts) (*Result, error)

// GetFunc fetches a single resource by (name, version). Version may be empty for the latest.
type GetFunc func(ctx context.Context, name, version string) (any, error)

// DeleteFunc deletes a single resource by (name, version). Version may be empty for all versions.
type DeleteFunc func(ctx context.Context, name, version string) error

// InitTemplateFunc writes a template YAML for `arctl init` to out.
type InitTemplateFunc func(ctx context.Context, out io.Writer, params InitParams) error

// BuildFunc performs build-side preparation (`arctl build`) for a document.
type BuildFunc func(ctx context.Context, doc *Document, params BuildParams) error

// ListFunc fetches all resources of this kind. Populated by CLI registrations.
type ListFunc func(ctx context.Context) ([]any, error)

// RowFunc renders a response item into table row strings for `arctl get`.
type RowFunc func(item any) []string

// ToResourceFunc converts an HTTP-client response item into a Document suitable
// for YAML/JSON output in `arctl get -o yaml`.
type ToResourceFunc func(item any) *Document

// Kind is a single registered resource type.
type Kind struct {
	// Identity
	Kind    string   // canonical, singular, lowercase: "agent"
	Plural  string   // "agents"
	Aliases []string // additional accepted kind strings on input

	// Typed schema (reflect.TypeOf(ConcreteSpec{}) — not a pointer type)
	SpecType reflect.Type

	// Service dispatch
	Apply  ApplyFunc
	Get    GetFunc    // optional
	Delete DeleteFunc // optional

	// CLI dispatch — populated by CLI registry or per-kind registration.
	ListFunc       ListFunc       // optional; fetches all items for `arctl get <kind>`
	RowFunc        RowFunc        // optional; renders a single item as table row
	ToResourceFunc ToResourceFunc // optional; converts response to Document for YAML/JSON output

	// CLI metadata
	TableColumns []Column
	InitTemplate InitTemplateFunc // optional
	Build        BuildFunc        // optional
}

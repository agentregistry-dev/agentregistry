// Package v1alpha1 defines the Kubernetes-style API types for all agentregistry
// resources.
//
// Every resource — Agent, MCPServer, Skill, Prompt, Deployment, Provider —
// uses the same envelope: apiVersion + kind + metadata + spec + status.
// These types are the single wire/storage/API contract propagating from a YAML
// manifest through the HTTP handler, Go client, service layer, and database
// row (spec+status as JSONB; metadata columns promoted). No intermediate DTOs,
// no translation functions.
//
// Typed objects (Agent, MCPServer, etc.) are the preferred handle. RawObject
// is the un-typed wire envelope used during apply dispatch when the kind is
// not yet known; use Scheme.Decode / Scheme.DecodeMulti to route into a typed
// object by kind.
package v1alpha1

// GroupVersion is the apiVersion string used by every resource in this package.
const GroupVersion = "ar.dev/v1alpha1"

// Canonical Kind names.
const (
	KindAgent      = "Agent"
	KindMCPServer  = "MCPServer"
	KindSkill      = "Skill"
	KindPrompt     = "Prompt"
	KindDeployment = "Deployment"
	KindProvider   = "Provider"
)

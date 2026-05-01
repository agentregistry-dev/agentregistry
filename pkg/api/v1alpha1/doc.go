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

import "strings"

// GroupVersion is the apiVersion string used by every resource in this package.
const GroupVersion = "ar.dev/v1alpha1"

// Canonical Kind names.
const (
	KindAgent           = "Agent"
	KindMCPServer       = "MCPServer"
	KindRemoteMCPServer = "RemoteMCPServer"
	KindSkill           = "Skill"
	KindPrompt          = "Prompt"
	KindDeployment      = "Deployment"
	KindProvider        = "Provider"
)

// BuiltinKinds is the stable ordered list of Kind names this package
// defines. Iteration order is deterministic; callers building parallel
// structures (table maps, route registrations, etc.) should range over
// this slice so they stay aligned as kinds are added. Enterprise-added
// kinds registered via Scheme.Register are NOT included here — those
// consumers bring their own list.
var BuiltinKinds = []string{
	KindAgent,
	KindMCPServer,
	KindRemoteMCPServer,
	KindSkill,
	KindPrompt,
	KindProvider,
	KindDeployment,
}

// PluralFor returns the lowercase route-plural for a Kind (e.g.
// "mcpservers" for KindMCPServer). Mirrors the convention the generic
// resource handler uses when cfg.PluralKind is empty: ToLower(kind) + "s".
// Enterprise / downstream builds registering additional kinds whose
// plural doesn't match this default should expose their own
// PluralFor helper; OSS callers use this one.
func PluralFor(kind string) string {
	return strings.ToLower(kind) + "s"
}

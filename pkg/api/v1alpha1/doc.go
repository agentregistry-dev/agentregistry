// Package v1alpha1 defines the Kubernetes-style API types for all agentregistry
// resources.
//
// Every resource — Agent, MCPServer, Skill, Prompt, Deployment, Runtime —
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

import (
	"strings"
	"sync"
)

// GroupVersion is the apiVersion string used by every resource in this package.
const GroupVersion = "ar.dev/v1alpha1"

// Canonical Kind names.
const (
	KindAgent      = "Agent"
	KindMCPServer  = "MCPServer"
	KindSkill      = "Skill"
	KindPrompt     = "Prompt"
	KindDeployment = "Deployment"
	KindRuntime    = "Runtime"
)

var (
	pluralOverridesMu sync.RWMutex
	pluralOverrides   = map[string]string{}
)

// RegisterPlural associates kind with the route plural used by the generic
// resource handlers. It is intended for downstream kinds whose plural does not
// match the default strings.ToLower(kind)+"s" convention.
func RegisterPlural(kind, plural string) error {
	return DefaultKindRegistry.UpdatePlural(kind, plural)
}

// MustRegisterPlural is RegisterPlural that panics on error. Use at init.
func MustRegisterPlural(kind, plural string) {
	if err := RegisterPlural(kind, plural); err != nil {
		panic(err)
	}
}

// PluralFor returns the route-plural for a Kind (e.g. "mcpservers" for
// KindMCPServer). By default it mirrors the convention the generic resource
// handler uses when cfg.PluralKind is empty: ToLower(kind) + "s". Downstream
// builds can override irregular plurals with RegisterPlural.
func PluralFor(kind string) string {
	if descriptor, ok := KindDescriptorFor(kind); ok && descriptor.Plural != "" {
		return descriptor.Plural
	}
	pluralOverridesMu.RLock()
	plural, ok := pluralOverrides[strings.ToLower(kind)]
	pluralOverridesMu.RUnlock()
	if ok {
		return plural
	}
	return defaultRoutePlural(kind)
}

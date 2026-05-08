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
	"errors"
	"fmt"
	"strings"
	"sync"
)

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
	KindRuntime         = "Runtime"
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
	KindRuntime,
	KindDeployment,
}

var (
	pluralOverridesMu sync.RWMutex
	pluralOverrides   = map[string]string{}
)

// RegisterPlural associates kind with the route plural used by the generic
// resource handlers. It is intended for downstream kinds whose plural does not
// match the default strings.ToLower(kind)+"s" convention.
func RegisterPlural(kind, plural string) error {
	kind = strings.TrimSpace(kind)
	plural = strings.TrimSpace(plural)
	if kind == "" {
		return errors.New("v1alpha1: cannot register plural for empty kind")
	}
	if plural == "" {
		return fmt.Errorf("v1alpha1: cannot register empty plural for kind %q", kind)
	}

	key := strings.ToLower(kind)
	pluralOverridesMu.Lock()
	defer pluralOverridesMu.Unlock()
	if existing, ok := pluralOverrides[key]; ok && existing != plural {
		return fmt.Errorf("v1alpha1: plural for kind %q already registered as %q", kind, existing)
	}
	pluralOverrides[key] = plural
	return nil
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
	pluralOverridesMu.RLock()
	plural, ok := pluralOverrides[strings.ToLower(kind)]
	pluralOverridesMu.RUnlock()
	if ok {
		return plural
	}
	return strings.ToLower(kind) + "s"
}

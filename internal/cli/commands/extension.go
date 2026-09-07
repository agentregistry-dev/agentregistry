package commands

import (
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// ExtensionKind describes a downstream v1alpha1 kind that should participate
// in the generic declarative CLI get/list/delete dispatch.
type ExtensionKind struct {
	Name          string
	Plural        string
	CanonicalKind string
	Aliases       []string
	TableColumns  []scheme.Column
	NewObject     func() v1alpha1.Object
	Row           func(v1alpha1.Object) []string
}

// RegisterExtensionKind registers a downstream v1alpha1 kind with the
// declarative get/list/delete commands. Apply/delete -f only need the
// v1alpha1.Default scheme registration; this hook covers explicit CLI
// commands like `arctl get accesspolicy NAME`.
func RegisterExtensionKind(k ExtensionKind) {
	scheme.Register(NewExtensionKind(k))
}

// NewExtensionKind converts a downstream extension kind into CLI dispatch
// metadata without mutating the package-global kind registry.
func NewExtensionKind(k ExtensionKind) *scheme.Kind {
	if k.Name == "" {
		panic("commands.RegisterExtensionKind: name is required")
	}
	if k.CanonicalKind == "" {
		k.CanonicalKind = k.Name
	}
	descriptor, ok := v1alpha1.KindDescriptorFor(k.CanonicalKind)
	if !ok {
		panic("commands.RegisterExtensionKind: v1alpha1 kind is not registered: " + k.CanonicalKind)
	}
	if len(k.TableColumns) == 0 {
		k.TableColumns = []scheme.Column{{Header: "NAME"}}
	}
	return newKindFromDescriptor(
		descriptor,
		k.Name,
		k.Plural,
		k.Aliases,
		k.TableColumns,
		k.NewObject,
		k.Row,
	)
}

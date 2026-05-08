package declarative

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
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
	if k.Name == "" {
		panic("declarative.RegisterExtensionKind: name is required")
	}
	if k.CanonicalKind == "" {
		k.CanonicalKind = k.Name
	}
	if k.NewObject == nil {
		k.NewObject = newSchemeObject(k.CanonicalKind)
	}
	if len(k.TableColumns) == 0 {
		k.TableColumns = []scheme.Column{{Header: "NAME"}}
	}

	scheme.Register(&scheme.Kind{
		Kind:         k.Name,
		Plural:       k.Plural,
		Aliases:      k.Aliases,
		TableColumns: k.TableColumns,
		ToYAMLFunc:   func(item any) any { return item },
		RowFunc: func(item any) []string {
			obj, ok := item.(v1alpha1.Object)
			if !ok {
				return []string{"<invalid>"}
			}
			if k.Row != nil {
				return k.Row(obj)
			}
			meta := obj.GetMetadata()
			return []string{meta.Name}
		},
		Get: func(ctx context.Context, name, _ string) (any, error) {
			return client.GetTyped(ctx, apiClient, k.CanonicalKind, v1alpha1.DefaultNamespace, name, "", k.NewObject)
		},
		ListFunc: func(ctx context.Context) ([]any, error) {
			return listLatestAny(ctx, k.CanonicalKind, k.NewObject)
		},
		Delete: func(ctx context.Context, name, version string, force bool) error {
			return deleteAny(ctx, k.CanonicalKind, name, version, force, k.NewObject)
		},
	})
}

func newSchemeObject(kind string) func() v1alpha1.Object {
	_, newAny, ok := v1alpha1.Default.Lookup(kind)
	if !ok {
		panic("declarative.RegisterExtensionKind: v1alpha1 kind is not registered: " + kind)
	}
	return func() v1alpha1.Object {
		obj, ok := newAny().(v1alpha1.Object)
		if !ok {
			panic("declarative.RegisterExtensionKind: object does not implement v1alpha1.Object: " + kind)
		}
		return obj
	}
}

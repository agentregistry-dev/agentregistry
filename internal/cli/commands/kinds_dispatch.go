package commands

// kinds_dispatch.go provides per-kind implementations of List, Get, TableRow, and
// YAML conversion for the declarative CLI commands (get/delete). All dispatch is driven
// by function fields on scheme.Kind, eliminating
// per-kind switch statements.

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

// errNotListable is returned by listItems for kinds that do not support list operations.
// Callers that iterate all kinds (e.g. "get all") should skip on this sentinel rather
// than treating it as an error.
var errNotListable = errors.New("list not supported for this kind")

func kindRegistry(deps cliruntime.Deps) *scheme.Registry {
	if deps.Kinds != nil {
		return deps.Kinds
	}
	return scheme.NewRegistry(scheme.All()...)
}

// namespaceAll is the list-only selector for every namespace the caller can
// view. It is never a namespace of its own.
const namespaceAll = "all"

// namespaceSelection is the namespace a command targets.
type namespaceSelection struct {
	// Namespace is never empty; without a selection it is the default namespace.
	Namespace string
	// Selected reports whether --namespace or ARCTL_NAMESPACE chose Namespace.
	// A selected namespace must match every NAMESPACE/NAME argument and every
	// metadata.namespace in an applied file.
	Selected bool
}

// selectedNamespace reads the namespace selection from the runtime.
func selectedNamespace(deps cliruntime.Deps) (namespaceSelection, error) {
	if deps.Runtime == nil {
		return namespaceSelection{Namespace: v1alpha1.DefaultNamespace}, nil
	}
	namespace, selected := deps.Runtime.Namespace()
	if namespace == namespaceAll {
		return namespaceSelection{}, fmt.Errorf("%q cannot be selected as a namespace; use -A/--all-namespaces to list every namespace", namespaceAll)
	}
	return namespaceSelection{Namespace: namespace, Selected: selected}, nil
}

type resourceLookupRef struct {
	Namespace string
	Name      string
}

// String renders the reference as NAME in the default namespace and
// NAMESPACE/NAME elsewhere, matching how users type it.
func (r resourceLookupRef) String() string {
	if r.Namespace == "" || r.Namespace == v1alpha1.DefaultNamespace {
		return r.Name
	}
	return r.Namespace + "/" + r.Name
}

// resolveResourceRef resolves a NAME or NAMESPACE/NAME argument against the
// selected namespace. NAME takes the selected namespace. NAMESPACE/NAME
// overrides an unselected default but must match a selected namespace.
func resolveResourceRef(arg string, sel namespaceSelection) (resourceLookupRef, error) {
	if arg == "" {
		return resourceLookupRef{}, fmt.Errorf("resource reference must be NAME or NAMESPACE/NAME")
	}
	namespace, name, ok := strings.Cut(arg, "/")
	if !ok {
		return resourceLookupRef{Namespace: sel.Namespace, Name: arg}, nil
	}
	if namespace == "" || name == "" || strings.Contains(name, "/") {
		return resourceLookupRef{}, fmt.Errorf("resource reference must be NAME or NAMESPACE/NAME")
	}
	if namespace == namespaceAll {
		return resourceLookupRef{}, fmt.Errorf("%q in %q is not a namespace; name one namespace", namespaceAll, arg)
	}
	if sel.Selected && namespace != sel.Namespace {
		return resourceLookupRef{}, fmt.Errorf("namespace %q in %q conflicts with selected namespace %q (--namespace or ARCTL_NAMESPACE)", namespace, arg, sel.Namespace)
	}
	return resourceLookupRef{Namespace: namespace, Name: name}, nil
}

// listItems fetches items for the given kind using its registered ListFunc.
// opts may be the zero value to list every row.
func listItems(ctx context.Context, c *client.Client, k *scheme.Kind, opts scheme.ListOpts) ([]any, error) {
	if k.ListFunc == nil {
		return nil, fmt.Errorf("%w: %q", errNotListable, k.Kind)
	}
	return k.ListFunc(ctx, c, opts)
}

// getItem fetches a single item by reference for the given kind. Empty tag
// resolves the latest tag; non-empty tag selects an exact tag on taggable
// artifacts.
func getItem(ctx context.Context, c *client.Client, k *scheme.Kind, ref resourceLookupRef, tag string) (any, error) {
	if k.Get == nil {
		return nil, fmt.Errorf("get not supported for kind %q", k.Kind)
	}
	return k.Get(ctx, c, ref.Namespace, ref.Name, tag)
}

// deleteItem deletes a single item by (reference, tag) for the given kind.
func deleteItem(ctx context.Context, c *client.Client, k *scheme.Kind, ref resourceLookupRef, tag string) error {
	if k.Delete == nil {
		return fmt.Errorf("delete not supported for kind %q", k.Kind)
	}
	return k.Delete(ctx, c, ref.Namespace, ref.Name, tag)
}

// listTags returns every live tag for (kind, reference). Errors when the kind
// is not a taggable artifact (e.g. mutable Deployment/Provider).
func listTags(ctx context.Context, c *client.Client, k *scheme.Kind, ref resourceLookupRef) ([]any, error) {
	if k.ListTags == nil {
		return nil, fmt.Errorf("--all-tags not supported for kind %q (resource is not taggable)", k.Kind)
	}
	return k.ListTags(ctx, c, ref.Namespace, ref.Name)
}

// deleteAllTags soft-deletes every live tag for (kind, reference). Errors when
// the kind is not a taggable artifact.
func deleteAllTags(ctx context.Context, c *client.Client, k *scheme.Kind, ref resourceLookupRef) error {
	if k.DeleteAllTags == nil {
		return fmt.Errorf("--all-tags not supported for kind %q (resource is not taggable)", k.Kind)
	}
	return k.DeleteAllTags(ctx, c, ref.Namespace, ref.Name)
}

// tableRow returns a []string row for the given item, matching the TableColumns
// registered in the kinds registry.
func tableRow(k *scheme.Kind, item any) []string {
	if k.RowFunc != nil {
		return k.RowFunc(item)
	}
	return []string{"<unknown kind>"}
}

// tableColumns returns the column header strings for the given kind.
func tableColumns(k *scheme.Kind) []string {
	headers := make([]string, len(k.TableColumns))
	for i, col := range k.TableColumns {
		headers[i] = col.Header
	}
	return headers
}

// toYAMLValue converts an item to the YAML/JSON value shown by `arctl get -o yaml|json`.
func toYAMLValue(k *scheme.Kind, item any) any {
	if k.ToYAMLFunc != nil {
		return k.ToYAMLFunc(item)
	}
	return nil
}

// kindPlural returns the plural display name for a kind, used in "No X found." messages.
func kindPlural(k *scheme.Kind) string {
	if k.Plural != "" {
		return k.Plural
	}
	return k.Kind + "s"
}

func supportedKindNames(kinds *scheme.Registry) string {
	names := make([]string, 0, len(kinds.All()))
	for _, kind := range kinds.All() {
		names = append(names, kindPlural(kind))
	}
	return strings.Join(names, ", ")
}

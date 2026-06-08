package declarative

// kinds_dispatch.go provides per-kind implementations of List, Get, TableRow, and
// YAML conversion for the declarative CLI commands (get/delete). All dispatch is driven
// by function fields on scheme.Kind, eliminating
// per-kind switch statements.

import (
	"context"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
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

// listItems fetches items for the given kind using its registered ListFunc.
// opts may be the zero value to list every row.
func listItems(ctx context.Context, c *client.Client, k *scheme.Kind, opts scheme.ListOpts) ([]any, error) {
	if k.ListFunc == nil {
		return nil, fmt.Errorf("%w: %q", errNotListable, k.Kind)
	}
	return k.ListFunc(ctx, c, opts)
}

// getItem fetches a single item by name for the given kind. Empty tag resolves
// the latest tag; non-empty tag selects an exact tag on taggable artifacts.
func getItem(ctx context.Context, c *client.Client, k *scheme.Kind, name, tag string) (any, error) {
	if k.Get == nil {
		return nil, fmt.Errorf("get not supported for kind %q", k.Kind)
	}
	return k.Get(ctx, c, name, tag)
}

// deleteItem deletes a single item by (name, tag) for the given kind.
func deleteItem(ctx context.Context, c *client.Client, k *scheme.Kind, name, tag string) error {
	if k.Delete == nil {
		return fmt.Errorf("delete not supported for kind %q", k.Kind)
	}
	return k.Delete(ctx, c, name, tag)
}

// listTags returns every live tag for (kind, name). Errors when the kind is not
// a taggable artifact (e.g. mutable Deployment/Provider).
func listTags(ctx context.Context, c *client.Client, k *scheme.Kind, name string) ([]any, error) {
	if k.ListTags == nil {
		return nil, fmt.Errorf("--all-tags not supported for kind %q (resource is not taggable)", k.Kind)
	}
	return k.ListTags(ctx, c, name)
}

// deleteAllTags soft-deletes every live tag for (kind, name). Errors when the
// kind is not a taggable artifact.
func deleteAllTags(ctx context.Context, c *client.Client, k *scheme.Kind, name string) error {
	if k.DeleteAllTags == nil {
		return fmt.Errorf("--all-tags not supported for kind %q (resource is not taggable)", k.Kind)
	}
	return k.DeleteAllTags(ctx, c, name)
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

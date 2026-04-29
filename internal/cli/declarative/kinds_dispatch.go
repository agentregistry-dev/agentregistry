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
)

// errNotListable is returned by listItems for kinds that do not support list operations.
// Callers that iterate all kinds (e.g. "get all") should skip on this sentinel rather
// than treating it as an error.
var errNotListable = errors.New("list not supported for this kind")

// listItems fetches all items for the given kind using its registered ListFunc.
func listItems(k *scheme.Kind) ([]any, error) {
	if k.ListFunc == nil {
		return nil, fmt.Errorf("%w: %q", errNotListable, k.Kind)
	}
	return k.ListFunc(context.Background())
}

// getItem fetches a single item by name for the given kind.
func getItem(k *scheme.Kind, name string) (any, error) {
	if k.Get == nil {
		return nil, fmt.Errorf("get not supported for kind %q", k.Kind)
	}
	return k.Get(context.Background(), name, "")
}

// deleteItem deletes a single item by (name, version) for the given kind.
// force=true asks the server to skip its PostDelete reconciliation hook
// (e.g. provider teardown for Deployment).
func deleteItem(k *scheme.Kind, name, version string, force bool) error {
	if k.Delete == nil {
		return fmt.Errorf("delete not supported for kind %q", k.Kind)
	}
	return k.Delete(context.Background(), name, version, force)
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

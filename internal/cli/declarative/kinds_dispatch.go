package declarative

// kinds_dispatch.go provides per-kind implementations of List, Get, TableRow, and
// ToResource for the declarative CLI commands (get/delete). All dispatch is driven
// by function fields on kinds.Kind (ListFunc, RowFunc, ToResourceFunc), eliminating
// per-kind switch statements.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
)

// errNotListable is returned by listItems for kinds that do not support list operations.
// Callers that iterate all kinds (e.g. "get all") should skip on this sentinel rather
// than treating it as an error.
var errNotListable = errors.New("list not supported for this kind")

// listItems fetches all items for the given kind using its registered ListFunc.
func listItems(k *kinds.Kind) ([]any, error) {
	if k.ListFunc == nil {
		return nil, fmt.Errorf("%w: %q", errNotListable, k.Kind)
	}
	return k.ListFunc(context.Background())
}

// getItem fetches a single item by name for the given kind.
func getItem(k *kinds.Kind, name string) (any, error) {
	if k.Get == nil {
		return nil, fmt.Errorf("get not supported for kind %q", k.Kind)
	}
	return k.Get(context.Background(), name, "")
}

// deleteItem deletes a single item by (name, version) for the given kind.
func deleteItem(k *kinds.Kind, name, version string) error {
	if k.Delete == nil {
		return fmt.Errorf("delete not supported for kind %q", k.Kind)
	}
	return k.Delete(context.Background(), name, version)
}

// tableRow returns a []string row for the given item, matching the TableColumns
// registered in the kinds registry.
func tableRow(k *kinds.Kind, item any) []string {
	if k.RowFunc != nil {
		return k.RowFunc(item)
	}
	return []string{"<unknown kind>"}
}

// tableColumns returns the column header strings for the given kind.
func tableColumns(k *kinds.Kind) []string {
	headers := make([]string, len(k.TableColumns))
	for i, col := range k.TableColumns {
		headers[i] = col.Header
	}
	return headers
}

// toResource converts an HTTP-client response item to a scheme.Resource (= kinds.Document)
// suitable for YAML/JSON output.
func toResource(k *kinds.Kind, item any) *scheme.Resource {
	if k.ToResourceFunc != nil {
		return k.ToResourceFunc(item)
	}
	return nil
}

// cleanServerFields removes server-managed fields that should not appear in the spec block.
func cleanServerFields(spec map[string]any) {
	delete(spec, "name")
	delete(spec, "version")
	delete(spec, "updatedAt")
	delete(spec, "status")
	delete(spec, "publishedAt")
}

// marshalToSpec is a helper that marshals an item to JSON and back to map[string]any,
// then strips server-managed fields.
func marshalToSpec(item any) map[string]any {
	b, _ := json.Marshal(item)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	cleanServerFields(spec)
	return spec
}

// kindPlural returns the plural display name for a kind, used in "No X found." messages.
func kindPlural(k *kinds.Kind) string {
	if k.Plural != "" {
		return k.Plural
	}
	return k.Kind + "s"
}

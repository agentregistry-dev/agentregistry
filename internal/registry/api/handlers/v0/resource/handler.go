// Package resource provides a single generic HTTP handler wiring for every
// v1alpha1 kind. One call to Register() binds namespace-scoped and cross-
// namespace endpoints for a kind, backed by a generic database.Store and a
// typed envelope T.
//
// Route shape (Kubernetes-inspired):
//
//	GET    {basePrefix}/{pluralKind}                                  list across all namespaces
//	GET    {basePrefix}/namespaces/{namespace}/{pluralKind}           list in one namespace
//	GET    {basePrefix}/namespaces/{namespace}/{pluralKind}/{name}    get latest (namespace, name)
//	GET    {basePrefix}/namespaces/{namespace}/{pluralKind}/{name}/{version}
//	PUT    {basePrefix}/namespaces/{namespace}/{pluralKind}/{name}/{version}
//	DELETE {basePrefix}/namespaces/{namespace}/{pluralKind}/{name}/{version}
package resource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Config is the per-kind configuration for Register. Every field is required.
type Config struct {
	// Kind is the canonical Kind name (e.g. v1alpha1.KindAgent = "Agent").
	Kind string
	// PluralKind is the lowercase plural used in route paths (e.g. "agents",
	// "mcpservers"). If empty, defaults to strings.ToLower(Kind) + "s".
	PluralKind string
	// BasePrefix is the HTTP route prefix shared across kinds (e.g. "/v0").
	// Routes extend it with `/{plural}` and `/namespaces/{ns}/{plural}/...`.
	BasePrefix string
	// Store is the database.Store bound to this kind's table. Callers
	// construct one Store per kind; this package does not create them.
	Store *database.Store
}

// Input/output wire types. Registered per-kind so OpenAPI schemas stay typed.

type getInput struct {
	Namespace string `path:"namespace"`
	Name      string `path:"name"`
	Version   string `path:"version"`
}

type getLatestInput struct {
	Namespace string `path:"namespace"`
	Name      string `path:"name"`
}

type deleteInput struct {
	Namespace string `path:"namespace"`
	Name      string `path:"name"`
	Version   string `path:"version"`
}

type listInput struct {
	Limit      int    `query:"limit" doc:"Max items to return (default 50)." default:"50"`
	Cursor     string `query:"cursor" doc:"Opaque pagination cursor."`
	Labels     string `query:"labels" doc:"Label selector: key=value,key2=value2."`
	LatestOnly bool   `query:"latestOnly" doc:"Only return rows with is_latest_version=true."`
	// IncludeTerminating surfaces soft-deleted rows (deletionTimestamp != nil)
	// which are hidden by default.
	IncludeTerminating bool `query:"includeTerminating" doc:"Include rows with a deletionTimestamp."`
}

// namespacedListInput is listInput + a namespace path segment. Separate
// struct because Huma reflects the whole input; mixing in a path tag on a
// cross-namespace list endpoint would make namespace mandatory there.
type namespacedListInput struct {
	Namespace          string `path:"namespace"`
	Limit              int    `query:"limit" doc:"Max items to return (default 50)." default:"50"`
	Cursor             string `query:"cursor" doc:"Opaque pagination cursor."`
	Labels             string `query:"labels" doc:"Label selector: key=value,key2=value2."`
	LatestOnly         bool   `query:"latestOnly" doc:"Only return rows with is_latest_version=true."`
	IncludeTerminating bool   `query:"includeTerminating" doc:"Include rows with a deletionTimestamp."`
}

type bodyOutput[T v1alpha1.Object] struct {
	Body T
}

type listOutput[T v1alpha1.Object] struct {
	Body struct {
		Items      []T    `json:"items"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
}

type putInput[T v1alpha1.Object] struct {
	Namespace string `path:"namespace"`
	Name      string `path:"name"`
	Version   string `path:"version"`
	Body      T
}

type deleteOutput struct{}

// Register wires the namespace-scoped + cross-namespace list endpoints for
// kind T. newObj must return a fresh, zero-valued T on each call (e.g.
// `func() *v1alpha1.Agent { return &v1alpha1.Agent{} }`).
func Register[T v1alpha1.Object](api huma.API, cfg Config, newObj func() T) {
	kind := cfg.Kind
	plural := cfg.PluralKind
	if plural == "" {
		plural = strings.ToLower(kind) + "s"
	}
	base := strings.TrimRight(cfg.BasePrefix, "/")

	crossNSList := base + "/" + plural
	nsList := base + "/namespaces/{namespace}/" + plural
	nsItem := nsList + "/{name}"
	nsItemVersion := nsItem + "/{version}"

	// Cross-namespace list: `/v0/{plural}`.
	huma.Register(api, huma.Operation{
		OperationID: "list-" + plural + "-all-namespaces",
		Method:      http.MethodGet,
		Path:        crossNSList,
		Summary:     fmt.Sprintf("List %s across all namespaces", kind),
	}, func(ctx context.Context, in *listInput) (*listOutput[T], error) {
		return runList(ctx, cfg, newObj, "", in.Labels, in.Limit, in.Cursor, in.LatestOnly, in.IncludeTerminating)
	})

	// Namespaced list: `/v0/namespaces/{ns}/{plural}`.
	huma.Register(api, huma.Operation{
		OperationID: "list-" + plural,
		Method:      http.MethodGet,
		Path:        nsList,
		Summary:     fmt.Sprintf("List %s in a namespace", kind),
	}, func(ctx context.Context, in *namespacedListInput) (*listOutput[T], error) {
		return runList(ctx, cfg, newObj, in.Namespace, in.Labels, in.Limit, in.Cursor, in.LatestOnly, in.IncludeTerminating)
	})

	// Get latest (namespace, name).
	huma.Register(api, huma.Operation{
		OperationID: "get-latest-" + strings.ToLower(kind),
		Method:      http.MethodGet,
		Path:        nsItem,
		Summary:     fmt.Sprintf("Get the latest version of a %s", kind),
	}, func(ctx context.Context, in *getLatestInput) (*bodyOutput[T], error) {
		row, err := cfg.Store.GetLatest(ctx, in.Namespace, in.Name)
		if err != nil {
			return nil, mapNotFound(err, kind, in.Namespace, in.Name, "")
		}
		obj, err := envelopeFromRow(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	// Get exact (namespace, name, version).
	huma.Register(api, huma.Operation{
		OperationID: "get-" + strings.ToLower(kind),
		Method:      http.MethodGet,
		Path:        nsItemVersion,
		Summary:     fmt.Sprintf("Get a %s by namespace, name, and version", kind),
	}, func(ctx context.Context, in *getInput) (*bodyOutput[T], error) {
		row, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version)
		if err != nil {
			return nil, mapNotFound(err, kind, in.Namespace, in.Name, in.Version)
		}
		obj, err := envelopeFromRow(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	// Apply (upsert).
	huma.Register(api, huma.Operation{
		OperationID:   "apply-" + strings.ToLower(kind),
		Method:        http.MethodPut,
		Path:          nsItemVersion,
		Summary:       fmt.Sprintf("Apply a %s (idempotent upsert)", kind),
		DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, in *putInput[T]) (*bodyOutput[T], error) {
		body := in.Body
		if apiVer := body.GetAPIVersion(); apiVer != "" && apiVer != v1alpha1.GroupVersion {
			return nil, huma.Error400BadRequest(fmt.Sprintf(
				"apiVersion %q is not supported; expected %q", apiVer, v1alpha1.GroupVersion))
		}
		if k := body.GetKind(); k != "" && k != kind {
			return nil, huma.Error400BadRequest(fmt.Sprintf(
				"kind %q does not match endpoint kind %q", k, kind))
		}
		meta := body.GetMetadata()
		if meta.Namespace != "" && meta.Namespace != in.Namespace {
			return nil, huma.Error400BadRequest("metadata.namespace does not match path")
		}
		if meta.Name != "" && meta.Name != in.Name {
			return nil, huma.Error400BadRequest("metadata.name does not match path")
		}
		if meta.Version != "" && meta.Version != in.Version {
			return nil, huma.Error400BadRequest("metadata.version does not match path")
		}

		specJSON, err := body.MarshalSpec()
		if err != nil {
			return nil, huma.Error400BadRequest("marshal spec: " + err.Error())
		}
		upsertOpts := database.UpsertOpts{}
		if meta.Finalizers != nil {
			upsertOpts.Finalizers = meta.Finalizers
		}
		if _, err := cfg.Store.Upsert(ctx, in.Namespace, in.Name, in.Version, specJSON, meta.Labels, upsertOpts); err != nil {
			return nil, huma.Error500InternalServerError("upsert "+kind, err)
		}

		row, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version)
		if err != nil {
			return nil, huma.Error500InternalServerError("read back "+kind, err)
		}
		obj, err := envelopeFromRow(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	// Delete (soft).
	huma.Register(api, huma.Operation{
		OperationID:   "delete-" + strings.ToLower(kind),
		Method:        http.MethodDelete,
		Path:          nsItemVersion,
		Summary:       fmt.Sprintf("Delete a %s (soft-delete: sets deletionTimestamp)", kind),
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *deleteInput) (*deleteOutput, error) {
		if err := cfg.Store.Delete(ctx, in.Namespace, in.Name, in.Version); err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, mapNotFound(err, kind, in.Namespace, in.Name, in.Version)
			}
			return nil, huma.Error500InternalServerError("delete "+kind, err)
		}
		return &deleteOutput{}, nil
	})
}

// runList is the shared list body used by both the cross-namespace and
// namespace-scoped list endpoints. Namespace="" means "across all namespaces".
func runList[T v1alpha1.Object](
	ctx context.Context, cfg Config, newObj func() T,
	namespace, labels string, limit int, cursor string, latestOnly, includeTerminating bool,
) (*listOutput[T], error) {
	opts := database.ListOpts{
		Namespace:          namespace,
		Limit:              limit,
		Cursor:             cursor,
		LatestOnly:         latestOnly,
		IncludeTerminating: includeTerminating,
	}
	if labels != "" {
		selector, err := parseLabelSelector(labels)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid labels selector: " + err.Error())
		}
		opts.LabelSelector = selector
	}
	rows, nextCursor, err := cfg.Store.List(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("list "+cfg.Kind, err)
	}
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		obj, err := envelopeFromRow(newObj, row, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		items = append(items, obj)
	}
	out := &listOutput[T]{}
	out.Body.Items = items
	out.Body.NextCursor = nextCursor
	return out, nil
}

// envelopeFromRow materializes a typed envelope from a *v1alpha1.RawObject.
// Stamps TypeMeta (apiVersion + kind), copies ObjectMeta + Status, and
// unmarshals the raw spec JSON into the typed Spec field.
func envelopeFromRow[T v1alpha1.Object](newObj func() T, row *v1alpha1.RawObject, kind string) (T, error) {
	out := newObj()
	out.SetTypeMeta(v1alpha1.TypeMeta{APIVersion: v1alpha1.GroupVersion, Kind: kind})
	out.SetMetadata(row.Metadata)
	out.SetStatus(row.Status)
	if len(row.Spec) > 0 {
		if err := out.UnmarshalSpec(row.Spec); err != nil {
			return out, fmt.Errorf("unmarshal spec: %w", err)
		}
	}
	return out, nil
}

// mapNotFound converts a pkgdb.ErrNotFound error into a Huma 404 with a
// consistent message. Other errors fall through as 500.
func mapNotFound(err error, kind, namespace, name, version string) error {
	if errors.Is(err, pkgdb.ErrNotFound) {
		if version == "" {
			return huma.Error404NotFound(fmt.Sprintf("%s %q/%q not found", kind, namespace, name))
		}
		return huma.Error404NotFound(fmt.Sprintf("%s %q/%q@%q not found", kind, namespace, name, version))
	}
	return huma.Error500InternalServerError("fetch "+kind, err)
}

// parseLabelSelector decodes "key=value,key2=value2" into a map. Values
// containing an `=` or `,` are not currently supported (no quoting rules
// yet); callers with that need should file a follow-up.
func parseLabelSelector(s string) (map[string]string, error) {
	out := make(map[string]string)
	for pair := range strings.SplitSeq(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		eq := strings.IndexByte(pair, '=')
		if eq <= 0 {
			return nil, fmt.Errorf("label %q must be key=value", pair)
		}
		key := strings.TrimSpace(pair[:eq])
		val := strings.TrimSpace(pair[eq+1:])
		if key == "" {
			return nil, fmt.Errorf("label %q has empty key", pair)
		}
		out[key] = val
	}
	return out, nil
}

// Ensure unused-import complaints stay quiet.
var _ = json.RawMessage(nil)

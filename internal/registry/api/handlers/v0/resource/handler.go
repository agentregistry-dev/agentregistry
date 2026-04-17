// Package resource provides a single generic HTTP handler wiring for every
// v1alpha1 kind. One call to Register() binds Get / GetLatest / List / Apply
// (PUT) / Delete endpoints at a path prefix, backed by a generic
// database.Store and typed envelope T.
//
// Today every kind gets its own handler package (internal/registry/api/handlers/v0/{agents,servers,skills,prompts,providers,deployments}/)
// duplicating near-identical CRUD plumbing. Those packages are replaced by
// six calls into this package, one per kind, during the PR 3 cutover.
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
	// PathPrefix is the HTTP route prefix (e.g. "/v0/agents"). Individual
	// endpoints extend this prefix with `/{name}` and `/{name}/{version}`.
	PathPrefix string
	// Store is the database.Store bound to this kind's table. Callers
	// construct one Store per kind; this package does not create them.
	Store *database.Store
}

// Input/output wire types. These are registered with Huma per kind, so each
// kind gets its own typed OpenAPI schema. `huma.Register` takes concrete
// type parameters, so we instantiate these generic wrappers at call time.

type getInput struct {
	Name    string `path:"name"`
	Version string `path:"version"`
}

type getLatestInput struct {
	Name string `path:"name"`
}

type deleteInput struct {
	Name    string `path:"name"`
	Version string `path:"version"`
}

type listInput struct {
	Limit      int    `query:"limit" doc:"Max items to return (default 50)." default:"50"`
	Cursor     string `query:"cursor" doc:"Opaque pagination cursor."`
	Labels     string `query:"labels" doc:"Label selector: key=value,key2=value2."`
	LatestOnly bool   `query:"latestOnly" doc:"Only return rows with is_latest_version=true."`
}

// bodyOutput[T] wraps a single typed envelope as the Huma response body.
type bodyOutput[T v1alpha1.Object] struct {
	Body T
}

// listOutput[T] wraps a paginated list of typed envelopes.
type listOutput[T v1alpha1.Object] struct {
	Body struct {
		Items      []T    `json:"items"`
		NextCursor string `json:"nextCursor,omitempty"`
	}
}

// putInput[T] carries the applied envelope plus path identity.
type putInput[T v1alpha1.Object] struct {
	Name    string `path:"name"`
	Version string `path:"version"`
	Body    T
}

type deleteOutput struct{}

// Register wires the five resource endpoints at cfg.PathPrefix for the typed
// envelope T. newObj must return a fresh, zero-valued T on each call (e.g.
// `func() *v1alpha1.Agent { return &v1alpha1.Agent{} }`).
//
// Endpoints:
//
//	GET    {prefix}                       → list (paginated, optional label + latest filters)
//	GET    {prefix}/{name}                → get latest version for a name
//	GET    {prefix}/{name}/{version}      → get exact (name, version)
//	PUT    {prefix}/{name}/{version}      → apply (upsert) — server-managed fields are populated on response
//	DELETE {prefix}/{name}/{version}      → remove
func Register[T v1alpha1.Object](api huma.API, cfg Config, newObj func() T) {
	lowerKind := strings.ToLower(cfg.Kind)
	pluralKind := lowerKind + "s"

	huma.Register(api, huma.Operation{
		OperationID: "list-" + pluralKind,
		Method:      http.MethodGet,
		Path:        cfg.PathPrefix,
		Summary:     fmt.Sprintf("List %s resources", cfg.Kind),
	}, func(ctx context.Context, in *listInput) (*listOutput[T], error) {
		opts := database.ListOpts{
			Limit:      in.Limit,
			Cursor:     in.Cursor,
			LatestOnly: in.LatestOnly,
		}
		if in.Labels != "" {
			selector, err := parseLabelSelector(in.Labels)
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
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-latest-" + lowerKind,
		Method:      http.MethodGet,
		Path:        cfg.PathPrefix + "/{name}",
		Summary:     fmt.Sprintf("Get the latest version of a %s", cfg.Kind),
	}, func(ctx context.Context, in *getLatestInput) (*bodyOutput[T], error) {
		row, err := cfg.Store.GetLatest(ctx, in.Name)
		if err != nil {
			return nil, mapNotFound(err, cfg.Kind, in.Name, "")
		}
		obj, err := envelopeFromRow(newObj, row, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-" + lowerKind,
		Method:      http.MethodGet,
		Path:        cfg.PathPrefix + "/{name}/{version}",
		Summary:     fmt.Sprintf("Get a %s by name and version", cfg.Kind),
	}, func(ctx context.Context, in *getInput) (*bodyOutput[T], error) {
		row, err := cfg.Store.Get(ctx, in.Name, in.Version)
		if err != nil {
			return nil, mapNotFound(err, cfg.Kind, in.Name, in.Version)
		}
		obj, err := envelopeFromRow(newObj, row, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "apply-" + lowerKind,
		Method:        http.MethodPut,
		Path:          cfg.PathPrefix + "/{name}/{version}",
		Summary:       fmt.Sprintf("Apply a %s (idempotent upsert)", cfg.Kind),
		DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, in *putInput[T]) (*bodyOutput[T], error) {
		body := in.Body
		if apiVer := body.GetAPIVersion(); apiVer != "" && apiVer != v1alpha1.GroupVersion {
			return nil, huma.Error400BadRequest(fmt.Sprintf(
				"apiVersion %q is not supported; expected %q", apiVer, v1alpha1.GroupVersion))
		}
		if kind := body.GetKind(); kind != "" && kind != cfg.Kind {
			return nil, huma.Error400BadRequest(fmt.Sprintf(
				"kind %q does not match endpoint kind %q", kind, cfg.Kind))
		}
		meta := body.GetMetadata()
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
		if _, err := cfg.Store.Upsert(ctx, in.Name, in.Version, specJSON, meta.Labels); err != nil {
			return nil, huma.Error500InternalServerError("upsert "+cfg.Kind, err)
		}

		row, err := cfg.Store.Get(ctx, in.Name, in.Version)
		if err != nil {
			return nil, huma.Error500InternalServerError("read back "+cfg.Kind, err)
		}
		obj, err := envelopeFromRow(newObj, row, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "delete-" + lowerKind,
		Method:        http.MethodDelete,
		Path:          cfg.PathPrefix + "/{name}/{version}",
		Summary:       fmt.Sprintf("Delete a %s", cfg.Kind),
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *deleteInput) (*deleteOutput, error) {
		if err := cfg.Store.Delete(ctx, in.Name, in.Version); err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, mapNotFound(err, cfg.Kind, in.Name, in.Version)
			}
			return nil, huma.Error500InternalServerError("delete "+cfg.Kind, err)
		}
		return &deleteOutput{}, nil
	})
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
func mapNotFound(err error, kind, name, version string) error {
	if errors.Is(err, pkgdb.ErrNotFound) {
		if version == "" {
			return huma.Error404NotFound(fmt.Sprintf("%s %q not found", kind, name))
		}
		return huma.Error404NotFound(fmt.Sprintf("%s %q@%q not found", kind, name, version))
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

// Ensure unused-import complaints stay quiet when we add encoding/json later.
var _ = json.RawMessage(nil)

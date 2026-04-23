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
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Config is the per-kind configuration for Register. Kind / BasePrefix /
// Store are required; Resolver is optional (enables cross-kind ref
// existence checks on apply).
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
	// Resolver is optional; when set, the apply handler calls
	// obj.ResolveRefs with it so dangling references surface as 400
	// errors. Leave nil to skip ref resolution (e.g. for kinds with no
	// ResourceRef fields).
	Resolver v1alpha1.ResolverFunc
	// RegistryValidator is optional; when set, the apply handler
	// calls obj.ValidateRegistries with it so external-registry
	// failures (package missing, OCI label mismatch, etc.) surface
	// as 400 errors. Leave nil to skip registry validation (tests,
	// offline imports, air-gapped servers).
	RegistryValidator v1alpha1.RegistryValidatorFunc
	// UniqueRemoteURLsChecker is optional; when set, the apply handler
	// calls obj.ValidateUniqueRemoteURLs with it so two objects of the
	// same Kind claiming the same remote URL surface as 409 errors.
	// Leave nil to skip (tests, single-tenant offline setups).
	UniqueRemoteURLsChecker v1alpha1.UniqueRemoteURLsFunc

	// PostUpsert is optional; when set, the apply handler invokes it
	// after a successful Upsert + read-back so the kind can drive
	// post-persist reconciliation. Deployment uses this to call
	// V1Alpha1Coordinator.Apply, which dispatches to the platform
	// adapter and patches status + finalizers.
	//
	// Hook errors surface as 500 — the row is already persisted, so a
	// failure here indicates degraded state the caller should retry.
	PostUpsert func(ctx context.Context, obj v1alpha1.Object) error

	// PostDelete is optional; when set, the delete handler invokes it
	// after Store.Delete (which sets DeletionTimestamp). The row still
	// exists at this point — finalizers keep it around. Deployment uses
	// this to call V1Alpha1Coordinator.Remove, which tears down runtime
	// resources and drops its finalizer so PurgeFinalized GC can
	// hard-delete the row.
	PostDelete func(ctx context.Context, obj v1alpha1.Object) error

	// SemanticSearch is optional; when set, the list handlers honor
	// `?semantic=<q>` + `?semanticThreshold=<f>` query params by
	// embedding the query string via this func and routing the result
	// through Store.SemanticList. Nil disables semantic search (the
	// query params return 400).
	SemanticSearch SemanticSearchFunc
}

// SemanticSearchFunc embeds a query string into a vector usable with
// Store.SemanticList. Constructed at bootstrap by wrapping an
// embeddings.Provider. nil disables `?semantic=` on list endpoints.
type SemanticSearchFunc func(ctx context.Context, query string) ([]float32, error)

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
	// Semantic, when non-empty, switches the list to semantic-search
	// mode: the query string is embedded via the server's provider,
	// results are ranked by cosine distance from the query vector,
	// and each item carries a score in listOutput.SemanticScores.
	// Requires the server to be built with embeddings enabled.
	Semantic          string  `query:"semantic" doc:"Semantic search query. Returns results ranked by similarity."`
	SemanticThreshold float32 `query:"semanticThreshold" doc:"Drop results with cosine distance above this threshold (0 = no filter)."`
}

// namespacedListInput is listInput + a namespace path segment. Separate
// struct because Huma reflects the whole input; a path tag on a
// cross-namespace list endpoint would make namespace mandatory there.
// (Tried to collapse via embedding; Huma's body/query schema reflection
// drops promoted fields, so the duplication stays.)
type namespacedListInput struct {
	Namespace          string  `path:"namespace"`
	Limit              int     `query:"limit" doc:"Max items to return (default 50)." default:"50"`
	Cursor             string  `query:"cursor" doc:"Opaque pagination cursor."`
	Labels             string  `query:"labels" doc:"Label selector: key=value,key2=value2."`
	LatestOnly         bool    `query:"latestOnly" doc:"Only return rows with is_latest_version=true."`
	IncludeTerminating bool    `query:"includeTerminating" doc:"Include rows with a deletionTimestamp."`
	Semantic           string  `query:"semantic" doc:"Semantic search query. Returns results ranked by similarity."`
	SemanticThreshold  float32 `query:"semanticThreshold" doc:"Drop results with cosine distance above this threshold (0 = no filter)."`
}

type bodyOutput[T v1alpha1.Object] struct {
	Body T
}

type listOutput[T v1alpha1.Object] struct {
	Body struct {
		Items      []T    `json:"items"`
		NextCursor string `json:"nextCursor,omitempty"`
		// SemanticScores is populated only when the list was ranked by
		// a `?semantic=<q>` query. Aligned with Items by index; score
		// is the cosine distance from the query vector (lower = closer).
		SemanticScores []float32 `json:"semanticScores,omitempty"`
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
		return runList(ctx, cfg, newObj, listParams{
			Namespace:          "",
			Labels:             in.Labels,
			Limit:              in.Limit,
			Cursor:             in.Cursor,
			LatestOnly:         in.LatestOnly,
			IncludeTerminating: in.IncludeTerminating,
			Semantic:           in.Semantic,
			SemanticThreshold:  in.SemanticThreshold,
		})
	})

	// Namespaced list: `/v0/namespaces/{ns}/{plural}`.
	huma.Register(api, huma.Operation{
		OperationID: "list-" + plural,
		Method:      http.MethodGet,
		Path:        nsList,
		Summary:     fmt.Sprintf("List %s in a namespace", kind),
	}, func(ctx context.Context, in *namespacedListInput) (*listOutput[T], error) {
		return runList(ctx, cfg, newObj, listParams{
			Namespace:          in.Namespace,
			Labels:             in.Labels,
			Limit:              in.Limit,
			Cursor:             in.Cursor,
			LatestOnly:         in.LatestOnly,
			IncludeTerminating: in.IncludeTerminating,
			Semantic:           in.Semantic,
			SemanticThreshold:  in.SemanticThreshold,
		})
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
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
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
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
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

		// Stamp path-derived identity into metadata so Validate sees the
		// resolved values (clients may omit namespace/name/version in the
		// body and rely on the path).
		meta.Namespace = in.Namespace
		meta.Name = in.Name
		meta.Version = in.Version
		body.SetMetadata(*meta)

		// Structural validation first — cheap, no I/O.
		if err := v1alpha1.ValidateObject(body); err != nil {
			return nil, huma.Error400BadRequest("validation: " + err.Error())
		}

		// Ref resolution — optional, cross-kind. Skipped when no resolver
		// is configured (e.g. kinds whose spec carries no ResourceRefs).
		if err := v1alpha1.ResolveObjectRefs(ctx, body, cfg.Resolver); err != nil {
			return nil, huma.Error400BadRequest("refs: " + err.Error())
		}

		// External-registry validation — optional, network-heavy.
		// Skipped when no validator is configured.
		if err := v1alpha1.ValidateObjectRegistries(ctx, body, cfg.RegistryValidator); err != nil {
			return nil, huma.Error400BadRequest("registries: " + err.Error())
		}

		// Cross-row remote-URL uniqueness. Optional; skipped when no
		// checker is configured. 409 Conflict is the right status — the
		// manifest is structurally valid but conflicts with existing state.
		if err := v1alpha1.ValidateObjectRemoteURLs(ctx, body, cfg.UniqueRemoteURLsChecker); err != nil {
			return nil, huma.Error409Conflict("remote urls: " + err.Error())
		}

		specJSON, err := body.MarshalSpec()
		if err != nil {
			return nil, huma.Error400BadRequest("marshal spec: " + err.Error())
		}
		upsertOpts := database.UpsertOpts{Labels: meta.Labels}
		if meta.Finalizers != nil {
			upsertOpts.Finalizers = meta.Finalizers
		}
		if meta.Annotations != nil {
			upsertOpts.Annotations = meta.Annotations
		}
		if _, err := cfg.Store.Upsert(ctx, in.Namespace, in.Name, in.Version, specJSON, upsertOpts); err != nil {
			return nil, huma.Error500InternalServerError("upsert "+kind, err)
		}

		row, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version)
		if err != nil {
			return nil, huma.Error500InternalServerError("read back "+kind, err)
		}
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}

		if cfg.PostUpsert != nil {
			if err := cfg.PostUpsert(ctx, obj); err != nil {
				return nil, huma.Error500InternalServerError(kind+" post-upsert", err)
			}
			// Re-read so the response reflects any status/finalizer writes
			// the post-upsert hook performed (e.g. Progressing condition,
			// adapter finalizer). Failure to re-read leaves the hook
			// changes invisible to the caller; degrade to the pre-hook
			// view rather than fail the already-successful apply.
			if refreshed, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version); err == nil {
				if refreshedObj, err := v1alpha1.EnvelopeFromRaw(newObj, refreshed, kind); err == nil {
					obj = refreshedObj
				}
			}
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
		// Pre-read so PostDelete has the final spec + finalizer set to
		// work with. Skipped when no hook is registered; a missing row
		// still surfaces as 404 via Store.Delete below.
		var preDelete T
		if cfg.PostDelete != nil {
			row, err := cfg.Store.Get(ctx, in.Namespace, in.Name, in.Version)
			if err != nil {
				return nil, mapNotFound(err, kind, in.Namespace, in.Name, in.Version)
			}
			obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
			if err != nil {
				return nil, huma.Error500InternalServerError("decode "+kind, err)
			}
			preDelete = obj
		}

		if err := cfg.Store.Delete(ctx, in.Namespace, in.Name, in.Version); err != nil {
			if errors.Is(err, pkgdb.ErrNotFound) {
				return nil, mapNotFound(err, kind, in.Namespace, in.Name, in.Version)
			}
			return nil, huma.Error500InternalServerError("delete "+kind, err)
		}

		if cfg.PostDelete != nil {
			if err := cfg.PostDelete(ctx, preDelete); err != nil {
				return nil, huma.Error500InternalServerError(kind+" post-delete", err)
			}
		}
		return &deleteOutput{}, nil
	})
}

// listParams bundles the query parameters the list endpoints accept.
// Shared across the cross-namespace and namespace-scoped list flows so
// adding a new parameter (semantic, threshold, future filters) touches
// one place instead of two call sites.
type listParams struct {
	Namespace          string
	Labels             string
	Limit              int
	Cursor             string
	LatestOnly         bool
	IncludeTerminating bool
	Semantic           string
	SemanticThreshold  float32
}

// runList is the shared list body used by both the cross-namespace and
// namespace-scoped list endpoints. Namespace="" means "across all namespaces".
// When p.Semantic is non-empty and cfg.SemanticSearch is set, the list
// routes through Store.SemanticList and returns items ranked by cosine
// distance with SemanticScores populated.
func runList[T v1alpha1.Object](
	ctx context.Context, cfg Config, newObj func() T, p listParams,
) (*listOutput[T], error) {
	if p.Semantic != "" {
		return runSemanticList(ctx, cfg, newObj, p)
	}

	opts := database.ListOpts{
		Namespace:          p.Namespace,
		Limit:              p.Limit,
		Cursor:             p.Cursor,
		LatestOnly:         p.LatestOnly,
		IncludeTerminating: p.IncludeTerminating,
	}
	if p.Labels != "" {
		selector, err := parseLabelSelector(p.Labels)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid labels selector: " + err.Error())
		}
		opts.LabelSelector = selector
	}
	rows, nextCursor, err := cfg.Store.List(ctx, opts)
	if err != nil {
		if errors.Is(err, database.ErrInvalidCursor) {
			return nil, huma.Error400BadRequest("invalid cursor")
		}
		return nil, huma.Error500InternalServerError("list "+cfg.Kind, err)
	}
	items := make([]T, 0, len(rows))
	for _, row := range rows {
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		v1alpha1.StripObjectReadmeContent(obj)
		items = append(items, obj)
	}
	out := &listOutput[T]{}
	out.Body.Items = items
	out.Body.NextCursor = nextCursor
	return out, nil
}

// runSemanticList handles `?semantic=<q>` ranking via the configured
// SemanticSearchFunc + Store.SemanticList. Disabled endpoints (nil
// SemanticSearch) return 400.
func runSemanticList[T v1alpha1.Object](
	ctx context.Context, cfg Config, newObj func() T, p listParams,
) (*listOutput[T], error) {
	if cfg.SemanticSearch == nil {
		return nil, huma.Error400BadRequest("semantic search is not enabled on this server")
	}
	vec, err := cfg.SemanticSearch(ctx, p.Semantic)
	if err != nil {
		return nil, huma.Error500InternalServerError("embed query: "+err.Error(), err)
	}
	opts := database.SemanticListOpts{
		Query:              vec,
		Threshold:          p.SemanticThreshold,
		Limit:              p.Limit,
		Namespace:          p.Namespace,
		LatestOnly:         p.LatestOnly,
		IncludeTerminating: p.IncludeTerminating,
	}
	if p.Labels != "" {
		selector, err := parseLabelSelector(p.Labels)
		if err != nil {
			return nil, huma.Error400BadRequest("invalid labels selector: " + err.Error())
		}
		opts.LabelSelector = selector
	}
	results, err := cfg.Store.SemanticList(ctx, opts)
	if err != nil {
		return nil, huma.Error500InternalServerError("semantic list "+cfg.Kind, err)
	}
	items := make([]T, 0, len(results))
	scores := make([]float32, 0, len(results))
	for _, r := range results {
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, r.Object, cfg.Kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+cfg.Kind, err)
		}
		v1alpha1.StripObjectReadmeContent(obj)
		items = append(items, obj)
		scores = append(scores, r.Score)
	}
	out := &listOutput[T]{}
	out.Body.Items = items
	out.Body.SemanticScores = scores
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
// may contain `=` (split is on the first `=` only); values with `,` are
// not supported and would split mid-pair.
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

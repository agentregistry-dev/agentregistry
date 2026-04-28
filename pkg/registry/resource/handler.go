// Package resource provides a single generic HTTP handler wiring for every
// v1alpha1 kind. One call to Register() binds the per-kind endpoints,
// backed by a generic v1alpha1store.Store and a typed envelope T.
//
// Route shape (flat; namespace is a query param, defaults to "default";
// `?namespace=all` widens list scope to every namespace):
//
//	GET    {basePrefix}/{pluralKind}?namespace={ns}                   list
//	GET    {basePrefix}/{pluralKind}/{name}?namespace={ns}            get latest
//	GET    {basePrefix}/{pluralKind}/{name}/{version}?namespace={ns}  get exact version
//	PUT    {basePrefix}/{pluralKind}/{name}/{version}?namespace={ns}  apply (idempotent upsert)
//	DELETE {basePrefix}/{pluralKind}/{name}/{version}?namespace={ns}  delete
package resource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// unescapePath URL-decodes a path segment captured by Huma. Resource
// names allow `/` (DNS-subdomain-style like `ai.exa/exa`) so callers
// pass them as `%2F`-escaped path segments. Huma keeps the raw path
// captures, so the handler must unescape before consulting the Store —
// otherwise rows stored as `ai.exa/exa` are unreachable via GET/DELETE.
// Returns a 400 on decode failure (malformed escape sequence).
func unescapePath(field, value string) (string, error) {
	out, err := url.PathUnescape(value)
	if err != nil {
		return "", huma.Error400BadRequest(fmt.Sprintf("invalid %s path segment: %v", field, err))
	}
	return out, nil
}

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
	// Routes extend it with `/{plural}/{name}/{version}`; namespace is
	// carried as a query param (`?namespace={ns}`, default "default").
	BasePrefix string
	// Store is the v1alpha1store.Store bound to this kind's table. Callers
	// construct one Store per kind; this package does not create them.
	Store *v1alpha1store.Store
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

	// PostUpsert is optional; when set, the apply handler invokes it
	// after a successful Upsert + read-back so the kind can drive
	// post-persist reconciliation. Deployment uses this to call
	// Coordinator.Apply, which dispatches to the platform
	// adapter and patches status.
	//
	// Hook errors surface as 500 — the row is already persisted, so a
	// failure here indicates degraded state the caller should retry.
	//
	// Known limitation (pre-Phase-2-KRT): Store.Upsert commits its own
	// transaction before the hook fires, so a hook failure leaves the
	// row persisted with stale Status (whatever the previous reconcile
	// wrote). The caller sees a 500, but a follow-up GetLatest still
	// returns the row.
	//
	// The hook re-fires on every PUT — including identical-spec
	// re-applies that are a no-op at the Store layer — because the
	// handler unconditionally invokes PostUpsert after Upsert
	// returns, without consulting the upsert change-status. This is
	// the operator-friendly retry path: a transient platform-adapter
	// failure clears as soon as the operator re-applies (or a periodic
	// CI re-apply succeeds), without forcing a spec bump.
	//
	// KRT will move this to an asynchronous reconcile loop with a
	// proper Pending → Failed condition transition; the contract is
	// pinned by TestResourceRegister_PostUpsertFailureLeavesPersistedRow.
	PostUpsert func(ctx context.Context, obj v1alpha1.Object) error

	// PostDelete is optional; when set, the delete handler invokes it
	// after Store.Delete (which sets DeletionTimestamp). The row still
	// exists at this point — the soft-delete + GC pass owns hard
	// removal. Deployment uses this hook to call
	// Coordinator.Remove, which tears down runtime resources
	// and writes the terminal Removed condition.
	PostDelete func(ctx context.Context, obj v1alpha1.Object) error

	// SemanticSearch is optional; when set, the list handlers honor
	// `?semantic=<q>` + `?semanticThreshold=<f>` query params by
	// embedding the query string via this func and routing the result
	// through Store.SemanticList. Nil disables semantic search (the
	// query params return 400).
	SemanticSearch SemanticSearchFunc

	// Authorize is optional; when set, every read and write handler
	// (get / list / apply / delete) invokes it as an access gate before
	// touching the store. Return nil to allow; return a huma error
	// (Error401Unauthorized / Error403Forbidden / etc.) to reject — the
	// value propagates back to the client as-is so the hook controls the
	// status code. Wrap a non-huma error in huma.Error500InternalServerError
	// if you want the server to 500.
	//
	// nil hook matches the OSS default: public reads and writes, with
	// authorization deferred to router-level middleware or the underlying
	// auth.AuthzProvider. Enterprise builds that need per-kind gates
	// (e.g. "only registry admins can mutate Role") wire this callback.
	//
	// The hook is called after path parsing and — for apply — after the
	// body decodes, but before any validation or store I/O. For list +
	// cross-namespace list, Name and Version are empty; for get-latest,
	// Version is empty; Object is non-nil only for apply.
	Authorize func(ctx context.Context, in AuthorizeInput) error

	// ListFilter is optional; when set, list handlers consult it before
	// querying the store and inject the returned predicate into
	// ListOpts.ExtraWhere / ExtraArgs. This is the per-row authz seam —
	// enterprise builds wire it to a per-user RBAC predicate so a
	// reader without grant for a given resource never sees the row in
	// the list response, but reads at the row endpoint still 403 via
	// Authorize.
	//
	// Returning a nil error + empty fragment means "no extra filter,
	// behave like the public default". A non-nil error short-circuits
	// the list and propagates to the caller (use a huma error to set
	// the response code; non-huma errors bubble as 500).
	//
	// Mirrors the contract on v1alpha1store.ListOpts.ExtraWhere — read
	// the placeholder + parameterization rules there before wiring a
	// new caller.
	ListFilter func(ctx context.Context, in AuthorizeInput) (extraWhere string, extraArgs []any, err error)
}

// AuthorizeInput is the context passed to Config.Authorize on every handler
// invocation. Fields are populated per the verb being authorized (see
// Config.Authorize comment for the combinations). New fields may be added
// in future releases — callers should use named-field initialization and
// tolerate unknown verbs by defaulting to deny.
type AuthorizeInput struct {
	// Verb is "get" | "list" | "apply" | "delete".
	Verb string
	// Kind is the canonical Kind the handler is serving (e.g. "Role").
	Kind string
	// Namespace is the URL-scoped namespace; empty for the cross-namespace
	// list endpoint.
	Namespace string
	// Name is empty for list verbs.
	Name string
	// Version is empty for list or get-latest.
	Version string
	// Object is non-nil only when Verb == "apply"; it carries the decoded
	// request body post-validation-stamping (path identity already merged
	// into metadata), so the hook can inspect labels / annotations / spec
	// in authz decisions.
	Object v1alpha1.Object
}

// SemanticSearchFunc embeds a query string into a vector usable with
// Store.SemanticList. Constructed at bootstrap by wrapping an
// embeddings.Provider. nil disables `?semantic=` on list endpoints.
type SemanticSearchFunc func(ctx context.Context, query string) ([]float32, error)

// Input/output wire types. Registered per-kind so OpenAPI schemas stay typed.
//
// Namespace is a `query:"namespace"` param (hidden from the user-facing
// API while the surface stays minimal; empty → "default", "all" → list
// across every namespace). Defaulting happens in resolveNamespace below
// so every endpoint sees the same semantics.

// namespaceAll is the query-param sentinel that asks the list endpoint
// to ignore the namespace scope and return rows from every namespace.
// Exported via listParams.Namespace == "" (empty string) to the Store.
const namespaceAll = "all"

// resolveNamespace applies the default-and-sentinel policy for
// ?namespace= query values: empty → DefaultNamespace, "all" → "" (the
// Store interprets empty as cross-namespace for list operations).
// Non-list callers (get/put/delete) still pass "" through as "default"
// — they never accept "all".
func resolveNamespace(raw string, allowAll bool) string {
	if allowAll && raw == namespaceAll {
		return ""
	}
	if raw == "" {
		return v1alpha1.DefaultNamespace
	}
	return raw
}

type getInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
	Version   string `path:"version"`
}

type getLatestInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
}

type deleteInput struct {
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
	Version   string `path:"version"`
	// Force=true skips the kind's PostDelete reconciliation hook
	// (e.g. provider teardown for Deployment) and only soft-deletes
	// the row. Useful for orphaned records whose external state is
	// already gone or unreachable.
	Force bool `query:"force" doc:"Skip provider-specific teardown and only remove the registry record." default:"false"`
}

type listInput struct {
	// Namespace scopes the list. Empty / missing → "default";
	// literal "all" → cross-namespace.
	Namespace  string `query:"namespace" doc:"Namespace (defaults to 'default'; 'all' lists across all namespaces)."`
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
	Namespace string `query:"namespace" doc:"Namespace (internal; defaults to 'default')."`
	Name      string `path:"name"`
	Version   string `path:"version"`
	Body      T
}

type deleteOutput struct{}

// Register wires the namespace-scoped + cross-namespace list endpoints for
// kind T. newObj must return a fresh, zero-valued T on each call (e.g.
// `func() *v1alpha1.Agent { return &v1alpha1.Agent{} }`).
//
// If T implements v1alpha1.ObjectWithReadme, the readme subresource
// routes (`/{plural}/{name}/readme` and the version-pinned variant)
// register automatically. Kinds without a Readme field on Spec
// (Provider, Deployment) don't satisfy the interface and skip readme
// registration silently. Readme routes register first so the literal
// `/{name}/readme` path beats the generic `/{name}/{version}` route at
// the shared depth.
func Register[T v1alpha1.Object](api huma.API, cfg Config, newObj func() T) {
	maybeRegisterReadmeRoutes(api, cfg, newObj)

	kind := cfg.Kind
	plural := cfg.PluralKind
	if plural == "" {
		plural = strings.ToLower(kind) + "s"
	}
	base := strings.TrimRight(cfg.BasePrefix, "/")

	// Flat URL shape: namespace is an internal detail carried as a query
	// param, not a path segment. Defaults to "default"; a special value
	// "all" on the list endpoint widens the scope to every namespace.
	listPath := base + "/" + plural
	itemPath := listPath + "/{name}"
	itemVersionPath := itemPath + "/{version}"

	// List: `/v0/{plural}?namespace=default` (or ?namespace=all).
	huma.Register(api, huma.Operation{
		OperationID: "list-" + plural,
		Method:      http.MethodGet,
		Path:        listPath,
		Summary:     fmt.Sprintf("List %s (scoped by ?namespace)", kind),
	}, func(ctx context.Context, in *listInput) (*listOutput[T], error) {
		ns := resolveNamespace(in.Namespace, true)
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, AuthorizeInput{Verb: "list", Kind: kind, Namespace: ns}); err != nil {
				return nil, err
			}
		}
		return runList(ctx, cfg, newObj, listParams{
			Namespace:          ns,
			Labels:             in.Labels,
			Limit:              in.Limit,
			Cursor:             in.Cursor,
			LatestOnly:         in.LatestOnly,
			IncludeTerminating: in.IncludeTerminating,
			Semantic:           in.Semantic,
			SemanticThreshold:  in.SemanticThreshold,
		})
	})

	// Get latest (name only; namespace via query).
	huma.Register(api, huma.Operation{
		OperationID: "get-latest-" + strings.ToLower(kind),
		Method:      http.MethodGet,
		Path:        itemPath,
		Summary:     fmt.Sprintf("Get the latest version of a %s", kind),
	}, func(ctx context.Context, in *getLatestInput) (*bodyOutput[T], error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, AuthorizeInput{Verb: "get", Kind: kind, Namespace: ns, Name: name}); err != nil {
				return nil, err
			}
		}
		row, err := cfg.Store.GetLatest(ctx, ns, name)
		if err != nil {
			return nil, mapNotFound(err, kind, ns, name, "")
		}
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	// Get exact (name, version; namespace via query).
	huma.Register(api, huma.Operation{
		OperationID: "get-" + strings.ToLower(kind),
		Method:      http.MethodGet,
		Path:        itemVersionPath,
		Summary:     fmt.Sprintf("Get a %s by name and version", kind),
	}, func(ctx context.Context, in *getInput) (*bodyOutput[T], error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		version, err := unescapePath("version", in.Version)
		if err != nil {
			return nil, err
		}
		if cfg.Authorize != nil {
			if err := cfg.Authorize(ctx, AuthorizeInput{Verb: "get", Kind: kind, Namespace: ns, Name: name, Version: version}); err != nil {
				return nil, err
			}
		}
		row, err := cfg.Store.Get(ctx, ns, name, version)
		if err != nil {
			return nil, mapNotFound(err, kind, ns, name, version)
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
		Path:          itemVersionPath,
		Summary:       fmt.Sprintf("Apply a %s (idempotent upsert)", kind),
		DefaultStatus: http.StatusOK,
	}, func(ctx context.Context, in *putInput[T]) (*bodyOutput[T], error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		version, err := unescapePath("version", in.Version)
		if err != nil {
			return nil, err
		}
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
		if meta.Namespace != "" && meta.Namespace != ns {
			return nil, huma.Error400BadRequest("metadata.namespace does not match ?namespace=")
		}
		if meta.Name != "" && meta.Name != name {
			return nil, huma.Error400BadRequest("metadata.name does not match path")
		}
		if meta.Version != "" && meta.Version != version {
			return nil, huma.Error400BadRequest("metadata.version does not match path")
		}

		// Stamp resolved identity into metadata so applyCore sees the
		// resolved values (clients may omit namespace/name/version in the
		// body and rely on the path + query).
		meta.Namespace = ns
		meta.Name = name
		meta.Version = version
		body.SetMetadata(*meta)

		if _, ae := applyCore(ctx, cfg.Store, body, applyOpts{
			Authorize:         cfg.Authorize,
			Resolver:          cfg.Resolver,
			RegistryValidator: cfg.RegistryValidator,
			PostUpsert:        cfg.PostUpsert,
		}, false); ae != nil {
			return nil, mapApplyErrorToHuma(ae, kind, ns, name, version)
		}

		// Read back so the response reflects the stored identity (assigned
		// generation, default'd metadata) plus any status / annotation
		// writes the PostUpsert hook performed. Failure to re-read after a
		// successful apply degrades to a 500 rather than swallowing the
		// already-persisted change silently.
		row, err := cfg.Store.Get(ctx, ns, name, version)
		if err != nil {
			return nil, huma.Error500InternalServerError("read back "+kind, err)
		}
		obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
		if err != nil {
			return nil, huma.Error500InternalServerError("decode "+kind, err)
		}
		return &bodyOutput[T]{Body: obj}, nil
	})

	// Delete (soft).
	huma.Register(api, huma.Operation{
		OperationID:   "delete-" + strings.ToLower(kind),
		Method:        http.MethodDelete,
		Path:          itemVersionPath,
		Summary:       fmt.Sprintf("Delete a %s (soft-delete: sets deletionTimestamp)", kind),
		DefaultStatus: http.StatusNoContent,
	}, func(ctx context.Context, in *deleteInput) (*deleteOutput, error) {
		ns := resolveNamespace(in.Namespace, false)
		name, err := unescapePath("name", in.Name)
		if err != nil {
			return nil, err
		}
		version, err := unescapePath("version", in.Version)
		if err != nil {
			return nil, err
		}

		// Pre-read so PostDelete has the final spec to work with.
		// Skipped when no hook is registered or when the caller asked
		// for ?force=true (skip provider teardown — orphan records /
		// unreachable backends).
		var preDelete v1alpha1.Object
		runHook := cfg.PostDelete != nil && !in.Force
		if runHook {
			row, err := cfg.Store.Get(ctx, ns, name, version)
			if err != nil {
				return nil, mapNotFound(err, kind, ns, name, version)
			}
			obj, err := v1alpha1.EnvelopeFromRaw(newObj, row, kind)
			if err != nil {
				return nil, huma.Error500InternalServerError("decode "+kind, err)
			}
			preDelete = obj
		}

		dopts := deleteOpts{
			Authorize:       cfg.Authorize,
			PreDeleteObject: preDelete,
		}
		if runHook {
			dopts.PostDelete = cfg.PostDelete
		}
		if ae := deleteCore(ctx, cfg.Store, kind, ns, name, version, dopts); ae != nil {
			return nil, mapApplyErrorToHuma(ae, kind, ns, name, version)
		}
		return &deleteOutput{}, nil
	})
}

// mapApplyErrorToHuma translates the stage-tagged applyError surface
// from applyCore / deleteCore into the huma error shape the
// single-resource handlers emit. Mirrors the per-stage HTTP-status
// policy that used to be inlined in each closure.
func mapApplyErrorToHuma(ae *applyError, kind, ns, name, version string) error {
	switch ae.Stage {
	case stageAuth:
		// Auth callbacks already return huma errors; propagate.
		return ae.Err
	case stageValidation:
		return huma.Error400BadRequest("validation: " + ae.Err.Error())
	case stageRefs:
		return huma.Error400BadRequest("refs: " + ae.Err.Error())
	case stageRegistries:
		return huma.Error400BadRequest("registries: " + ae.Err.Error())
	case stageMarshal:
		return huma.Error400BadRequest("marshal spec: " + ae.Err.Error())
	case stageUpsert:
		if ae.Terminating {
			return huma.Error409Conflict(fmt.Sprintf(
				"%s %s/%s/%s is terminating; delete + re-apply once GC purges the row",
				kind, ns, name, version))
		}
		return huma.Error500InternalServerError("upsert "+kind, ae.Err)
	case stagePostUpsert:
		return huma.Error500InternalServerError(kind+" post-upsert", ae.Err)
	case stageDelete:
		if ae.NotFound {
			return mapNotFound(ae.Err, kind, ns, name, version)
		}
		return huma.Error500InternalServerError("delete "+kind, ae.Err)
	case stagePostDelete:
		return huma.Error500InternalServerError(kind+" post-delete", ae.Err)
	}
	return huma.Error500InternalServerError(kind+" "+string(ae.Stage), ae.Err)
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

	opts := v1alpha1store.ListOpts{
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
	if cfg.ListFilter != nil {
		extra, extraArgs, err := cfg.ListFilter(ctx, AuthorizeInput{Verb: "list", Kind: cfg.Kind, Namespace: p.Namespace})
		if err != nil {
			return nil, err
		}
		opts.ExtraWhere = extra
		opts.ExtraArgs = extraArgs
	}
	rows, nextCursor, err := cfg.Store.List(ctx, opts)
	if err != nil {
		if errors.Is(err, v1alpha1store.ErrInvalidCursor) {
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
	opts := v1alpha1store.SemanticListOpts{
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
	// Row-level RBAC seam — same as runList. Without this, ?semantic=
	// would rank denied rows by similarity and leak existence + score.
	if cfg.ListFilter != nil {
		extra, extraArgs, err := cfg.ListFilter(ctx, AuthorizeInput{Verb: "list", Kind: cfg.Kind, Namespace: p.Namespace})
		if err != nil {
			return nil, err
		}
		opts.ExtraWhere = extra
		opts.ExtraArgs = extraArgs
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

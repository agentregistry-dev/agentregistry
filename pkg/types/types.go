// Package types holds extension-point surfaces that cross the
// pkg/registry <-> internal/registry boundary. Anything a downstream
// build (out-of-tree wrapper, custom CLI) needs to implement to plug
// into the registry app lives here.
//
// The types are split by domain across files:
//   - types.go     — AppOptions, Server, HTTPServerFactory,
//     Response/EmptyResponse wrappers
//   - adapter.go   — deployment + provider adapter surfaces
//     (DeploymentAdapter, ProviderPlatformAdapter)
//   - daemon.go    — CLI-side daemon + token provider hooks
package types

import (
	"context"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

// DatabaseFactory is a function type that creates a store implementation.
// This allows implementors to run additional migrations and wrap the base
// store.
type DatabaseFactory func(ctx context.Context, databaseURL string, baseStore database.Store, authz auth.Authorizer) (database.Store, error)

// AuthorizeInput is the per-call context handed to
// Authorizer + ListFilter callbacks. Mirrors
// resource.AuthorizeInput field-for-field; declared here to keep
// AppOptions free of internal-package imports.
type AuthorizeInput struct {
	// Verb is one of "get", "list", "apply", "delete".
	Verb string
	// Kind is the canonical Kind name (v1alpha1.KindAgent, etc.).
	Kind string
	// Namespace is the URL-scoped namespace; "" for cross-namespace list.
	Namespace string
	// Name is the resource name; "" for list verbs.
	Name string
	// Tag is the resource tag for content kinds; "" for list/get-latest.
	Tag string
}

// Authorizer gates a single resource handler invocation. Return
// nil to allow; a huma error to set the response status; any other
// error to surface as 500. Wired into resource.Config.Authorize.
type Authorizer func(ctx context.Context, in AuthorizeInput) error

// ListFilter returns a SQL predicate fragment + bind args to
// inject into the list query as ListOpts.ExtraWhere / ExtraArgs. Wired
// into resource.Config.ListFilter. Return ("", nil, nil) for "no
// filter"; non-nil err short-circuits the list.
type ListFilter func(ctx context.Context, in AuthorizeInput) (extraWhere string, extraArgs []any, err error)

// PostUpsert runs after a successful PUT or apply on a v1alpha1
// resource. Wired into resource.Config.PostUpsert and the matching
// per-doc apply hook on /v0/apply. Hook errors propagate to the
// caller (500 on the per-kind PUT path, ApplyStatusFailed on the
// batch path).
type PostUpsert func(ctx context.Context, obj v1alpha1.Object) error

// PostDelete runs after a successful DELETE on a v1alpha1
// resource. Wired into resource.Config.PostDelete + the apply
// batch's per-doc delete hook.
type PostDelete func(ctx context.Context, obj v1alpha1.Object) error

// CreateStagerInput is handed to an optional downstream create-approval hook.
// Store is intentionally any to keep pkg/types independent from the concrete
// resource store package; downstream integrations can type-assert when needed.
type CreateStagerInput struct {
	Kind      string
	Namespace string
	Name      string
	Tag       string
	Object    v1alpha1.Object
	Store     any
}

// CreateStagerResult reports whether the create was staged instead of written
// to production storage.
type CreateStagerResult struct {
	Staged bool
}

// ResourceRouteContext exposes the finalized v1alpha1 route wiring to
// downstream integrations that need to register adjacent routes against
// the same stores, resolver, validator, and post-persist hooks.
type ResourceRouteContext struct {
	// Stores is the finalized per-kind store map. It is intentionally any to
	// avoid a public package import cycle with the concrete store package.
	Stores            any
	Resolver          v1alpha1.ResolverFunc
	RegistryValidator v1alpha1.RegistryValidatorFunc
	PostUpserts       map[string]func(context.Context, v1alpha1.Object) error
	PostDeletes       map[string]func(context.Context, v1alpha1.Object) error
	InitialFinalizers map[string]func(v1alpha1.Object) []string
}

// Auditor receives audit events for state changes that the OSS layer
// considers significant. The default OSS implementation is a no-op;
// downstream builds plug in a real audit sink via NewStore options.
//
// Audit completeness is enforced at the source: every code path that
// produces a recordable state change calls into Auditor directly,
// rather than relying on observers (PostUpsert hooks, etc.) to remember
// to log.
type Auditor interface {
	// ResourceTagCreated is invoked when Store.Upsert creates a new tag row
	// for a content-registry kind. Mutable-object kinds do not produce this
	// event.
	ResourceTagCreated(ctx context.Context, kind, namespace, name, tag string)
}

type noopAuditor struct{}

func (noopAuditor) ResourceTagCreated(ctx context.Context, kind, namespace, name, tag string) {
}

// NoopAuditor is the default Auditor used when none is plugged in.
var NoopAuditor Auditor = noopAuditor{}

// AppOptions contains configuration for the registry app.
// All fields are optional and allow external developers to extend
// functionality.
//
// This type lives in pkg/types (rather than pkg/registry or
// internal/registry) so that both the public entrypoint
// (pkg/registry/registry_app.go) and the internal implementation
// (internal/registry/registry_app.go) can reference it without a cyclic
// import.
type AppOptions struct {
	// DatabaseFactory is an optional function to create a database that
	// adds new functionality. The factory receives the base database and
	// can run additional migrations. If nil, uses the default PostgreSQL
	// database.
	DatabaseFactory DatabaseFactory

	// ProviderPlatforms registers per-platform PostUpsert/PostDelete
	// hooks for the KindProvider resource handler, keyed by
	// Provider.Spec.Platform ("aws", "gcp", "kagent", ...). Used by
	// downstream builds to mirror Provider apply/delete into a
	// platform-specific sidecar table (aws_connections,
	// gcp_connections, kagent_connections, etc.). Missing platforms =
	// no sidecar reconciliation for that platform — the v1alpha1
	// Provider row still persists.
	ProviderPlatforms map[string]ProviderPlatformAdapter

	// DeploymentAdapters registers v1alpha1 DeploymentAdapter
	// implementations keyed by Provider.Spec.Platform ("local",
	// "kubernetes", ...). The reconciler/coordinator looks up by platform
	// string; downstream builds inject additional adapters here.
	DeploymentAdapters map[string]DeploymentAdapter

	// Authorizers gates every read + write operation on the
	// generic v1alpha1 resource handler, keyed by canonical Kind name
	// (v1alpha1.KindAgent, v1alpha1.KindMCPServer, etc.). Downstream
	// builds wire their RBAC engine here so reader / publisher / admin
	// gates fire on the OSS-registered Agent / MCPServer / Skill /
	// Prompt / Provider / Deployment endpoints. Missing keys behave
	// like "no per-kind gate" — the resource handler's default permits
	// the call, with API-level authn middleware still applying.
	Authorizers map[string]Authorizer

	// ListFilters injects per-kind ExtraWhere predicates into
	// list queries. Use this for row-level visibility (e.g. RBAC
	// filtering: a reader without a grant for a given resource never
	// sees the row in a list response). The (string, []any) tuple is
	// passed straight through to v1alpha1store.ListOpts.ExtraWhere /
	// ExtraArgs — see that docstring for placeholder rules.
	ListFilters map[string]ListFilter

	// PostUpserts run after the generic resource handler PUTs a
	// row, per kind. Downstream builds wire this for kinds that need
	// platform side-effects on apply — Provider apply mirroring spec
	// into a per-platform sidecar table, for example. Missing keys =
	// no post-upsert hook for that kind.
	//
	// Hook errors fail the request with 500 (the row is already
	// persisted, so a hook failure indicates degraded state).
	PostUpserts map[string]PostUpsert

	// PostDeletes mirror PostUpserts on the delete path.
	PostDeletes map[string]PostDelete

	// CreateStager optionally intercepts validated creates before the row
	// reaches production storage. Downstream builds use this for native
	// approval staging; nil preserves normal direct writes.
	CreateStager func(ctx context.Context, in CreateStagerInput) (CreateStagerResult, error)

	// ResolverWrapper can decorate the shared v1alpha1 ResourceRef resolver
	// before route registration. Downstream approval integrations use this to allow
	// same-submit pending references to validate without writing them to
	// production storage first. Nil preserves the default store-backed resolver.
	ResolverWrapper func(v1alpha1.ResolverFunc) v1alpha1.ResolverFunc

	// V1Alpha1StoreTables registers additional v1alpha1 kinds with their
	// backing PostgreSQL tables. Downstream builds that add their own
	// Scheme kinds should populate this so the shared /v0/apply,
	// /v0/import, resolver, and generic route plumbing can see the same
	// store map as any ExtraRoutes they register.
	V1Alpha1StoreTables map[string]string

	// V1Alpha1MutableStoreKinds marks extra v1alpha1 kinds that use mutable
	// namespace/name object behavior instead of tagged artifact semantics.
	// Downstream control-plane/config kinds are v1alpha1-shaped but are not
	// content artifacts.
	V1Alpha1MutableStoreKinds map[string]bool

	// RegistryValidator overrides the per-package registry
	// validator (the dispatcher consulted on apply / import to confirm
	// each declared package — npm / pypi / oci / nuget / mcpb — exists
	// and (for OCI) carries the
	// `LABEL io.modelcontextprotocol.server.name` ownership annotation
	// proving the publisher controls the OCI namespace).
	//
	// Default (nil) is registries.Dispatcher, which fans out to every
	// per-registry validator and matches the public-catalogue contract
	// the upstream modelcontextprotocol/registry project ships. That's
	// the right behavior for the OSS public catalogue but not for
	// private deployments where:
	//
	//   - images live in private ECR / GCR / ACR that anonymous fetch
	//     can't reach;
	//   - server names aren't claims against a public namespace, so the
	//     ownership-annotation requirement is moot;
	//   - synthetic test names mean no public image can satisfy the
	//     annotation match.
	//
	// Pass a custom RegistryValidatorFunc to filter out registry types
	// the build doesn't want enforced (e.g. wrap registries.Dispatcher
	// and short-circuit RegistryTypeOCI to nil), or pass an explicit
	// no-op (`func(...) error { return nil }`) to disable per-package
	// registry validation entirely. Cross-kind ResourceRef checks still
	// run regardless.
	RegistryValidator v1alpha1.RegistryValidatorFunc

	// ExtraRoutes allows external integrations to register additional HTTP
	// routes using the same API instance and path prefix as OSS core
	// routes.
	ExtraRoutes func(api huma.API, pathPrefix string)

	// ExtraResourceRoutes allows external integrations to register routes
	// after the generic v1alpha1 resource context has been finalized.
	ExtraResourceRoutes func(api huma.API, pathPrefix string, ctx ResourceRouteContext)

	// HTTPServerFactory is an optional function to create a server that
	// adds new API routes.
	HTTPServerFactory HTTPServerFactory

	// OnHTTPServerCreated is an optional callback that receives the
	// created server (potentially extended via HTTPServerFactory).
	OnHTTPServerCreated func(Server)

	// UIHandler is an optional HTTP handler for serving a custom UI at
	// the root path ("/"). If provided, this handler will be used instead
	// of the default redirect to docs. API routes will still take
	// precedence over the UI handler.
	UIHandler http.Handler

	// AuthnProvider is an optional authentication provider.
	AuthnProvider auth.AuthnProvider

	// AuthzProvider is an optional authorization provider.
	AuthzProvider auth.AuthzProvider

	// Auditor receives audit events from the v1alpha1 store layer
	// (e.g. ResourceTagCreated on Upsert creates). The default OSS
	// behavior is a no-op; downstream builds plug in a real audit sink.
	// If nil, NoopAuditor is used.
	Auditor Auditor

	// InitialFinalizers seeds finalizers atomically on create for kinds
	// whose external teardown must be protected from a concurrent delete.
	InitialFinalizers map[string]func(v1alpha1.Object) []string
}

// Server represents the HTTP server and provides access to the Huma API
// and HTTP mux for registering new routes and handlers.
//
// This interface allows external packages to extend the server
// functionality by adding new endpoints without accessing internal
// implementation details.
type Server interface {
	// HumaAPI returns the Huma API instance, allowing registration of new
	// routes that will appear in the OpenAPI documentation.
	HumaAPI() huma.API

	// Mux returns the HTTP ServeMux, allowing registration of custom HTTP
	// handlers.
	Mux() *http.ServeMux

	// Start begins listening for incoming HTTP requests.
	Start() error

	// Shutdown gracefully shuts down the server.
	Shutdown(ctx context.Context) error
}

// HTTPServerFactory is a function type that creates a server
// implementation that adds new API routes and handlers.
//
// The factory receives a Server interface and should return a Server
// after registering new routes using base.HumaAPI() or base.Mux().
type HTTPServerFactory func(base Server, store database.Store) Server

// Response is a generic wrapper for Huma responses.
// Usage: Response[HealthBody] instead of HealthOutput.
type Response[T any] struct {
	Body T
}

// EmptyResponse represents a simple success response with a message.
type EmptyResponse struct {
	Message string `json:"message" doc:"Success message" example:"Operation completed successfully"`
}

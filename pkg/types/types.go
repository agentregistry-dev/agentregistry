// Package types holds extension-point surfaces that cross the
// pkg/registry <-> internal/registry boundary. Anything a downstream
// build (enterprise wrapper, custom CLI) needs to implement to plug
// into the registry app lives here.
//
// The types are split by domain across files:
//   - types.go              — AppOptions, Server, HTTPServerFactory,
//     Response/EmptyResponse wrappers
//   - adapter_v1alpha1.go   — v1alpha1 deployment + provider adapter
//     surfaces (DeploymentAdapter, ProviderPlatformAdapter)
//   - daemon.go             — CLI-side daemon + token provider hooks
package types

import (
	"context"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
)

// DatabaseFactory is a function type that creates a store implementation.
// This allows implementors to run additional migrations and wrap the base
// store.
type DatabaseFactory func(ctx context.Context, databaseURL string, baseStore database.Store, authz auth.Authorizer) (database.Store, error)

// V1Alpha1AuthorizeInput is the per-call context handed to
// V1Alpha1Authorizer + V1Alpha1ListFilter callbacks. Mirrors
// resource.AuthorizeInput field-for-field; declared here to keep
// AppOptions free of internal-package imports.
type V1Alpha1AuthorizeInput struct {
	// Verb is one of "get", "list", "apply", "delete".
	Verb string
	// Kind is the canonical Kind name (v1alpha1.KindAgent, etc.).
	Kind string
	// Namespace is the URL-scoped namespace; "" for cross-namespace list.
	Namespace string
	// Name is the resource name; "" for list verbs.
	Name string
	// Version is the resource version; "" for list and get-latest.
	Version string
}

// V1Alpha1Authorizer gates a single resource handler invocation. Return
// nil to allow; a huma error to set the response status; any other
// error to surface as 500. Wired into resource.Config.Authorize.
type V1Alpha1Authorizer func(ctx context.Context, in V1Alpha1AuthorizeInput) error

// V1Alpha1ListFilter returns a SQL predicate fragment + bind args to
// inject into the list query as ListOpts.ExtraWhere / ExtraArgs. Wired
// into resource.Config.ListFilter. Return ("", nil, nil) for "no
// filter"; non-nil err short-circuits the list.
type V1Alpha1ListFilter func(ctx context.Context, in V1Alpha1AuthorizeInput) (extraWhere string, extraArgs []any, err error)

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

	// ProviderPlatforms registers adapters for provider CRUD by provider
	// platform type.
	ProviderPlatforms map[string]ProviderPlatformAdapter

	// DeploymentAdapters registers v1alpha1 DeploymentAdapter
	// implementations keyed by Provider.Spec.Platform ("local",
	// "kubernetes", ...). The reconciler/coordinator looks up by platform
	// string; enterprise builds inject additional adapters here.
	DeploymentAdapters map[string]DeploymentAdapter

	// V1Alpha1Authorizers gates every read + write operation on the
	// generic v1alpha1 resource handler, keyed by canonical Kind name
	// (v1alpha1.KindAgent, v1alpha1.KindMCPServer, etc.). Enterprise
	// builds wire their RBAC engine here so reader / publisher / admin
	// gates fire on the OSS-registered Agent / MCPServer / Skill /
	// Prompt / Provider / Deployment endpoints. Missing keys behave
	// like "no per-kind gate" — the resource handler's default permits
	// the call, with API-level authn middleware still applying.
	V1Alpha1Authorizers map[string]V1Alpha1Authorizer

	// V1Alpha1ListFilters injects per-kind ExtraWhere predicates into
	// list queries. Use this for row-level visibility (e.g. RBAC
	// filtering: a reader without a grant for a given resource never
	// sees the row in a list response). The (string, []any) tuple is
	// passed straight through to v1alpha1store.ListOpts.ExtraWhere /
	// ExtraArgs — see that docstring for placeholder rules.
	V1Alpha1ListFilters map[string]V1Alpha1ListFilter

	// ExtraRoutes allows external integrations to register additional HTTP
	// routes using the same API instance and path prefix as OSS core
	// routes.
	ExtraRoutes func(api huma.API, pathPrefix string)

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

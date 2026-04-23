package types

import (
	"context"
	"errors"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/danielgtaylor/huma/v2"
	"github.com/spf13/cobra"
)

// ErrCLINoStoredToken is returned when no stored authentication token is found.
// This is expected for CLI commands that do not require authentication (e.g. artifact init).
var ErrCLINoStoredToken = errors.New("no stored authentication token")

// ErrNoOIDCDefined is returned when OIDC is not defined.
// This is expected for CLI commands that do not require authentication (e.g. artifact init) and a user/extension does not define OIDC.
var ErrNoOIDCDefined = errors.New("OIDC is not defined")

// ProviderPlatformAdapter defines provider CRUD behavior for a provider platform type.
type ProviderPlatformAdapter interface {
	Platform() string
	ListProviders(ctx context.Context) ([]*v1alpha1.Provider, error)
	CreateProvider(ctx context.Context, provider *v1alpha1.Provider) (*v1alpha1.Provider, error)
	GetProvider(ctx context.Context, providerID string) (*v1alpha1.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, provider *v1alpha1.Provider) (*v1alpha1.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}

// DatabaseFactory is a function type that creates a store implementation.
// This allows implementors to run additional migrations and wrap the base store.
type DatabaseFactory func(ctx context.Context, databaseURL string, baseStore database.Store, authz auth.Authorizer) (database.Store, error)

// AppOptions contains configuration for the registry app.
// All fields are optional and allow external developers to extend functionality.
//
// This type lives in pkg/types (rather than pkg/registry or internal/registry)
// so that both the public entrypoint (pkg/registry/registry_app.go) and the
// internal implementation (internal/registry/registry_app.go) can reference
// it without a cyclic import.
type AppOptions struct {
	// DatabaseFactory is an optional function to create a database that adds new functionality.
	// The factory receives the base database and can run additional migrations.
	// If nil, uses the default PostgreSQL database.
	DatabaseFactory DatabaseFactory

	// ProviderPlatforms registers adapters for provider CRUD by provider platform type.
	ProviderPlatforms map[string]ProviderPlatformAdapter

	// DeploymentAdapters registers v1alpha1 DeploymentAdapter implementations
	// keyed by Provider.Spec.Platform ("local", "kubernetes", ...). The
	// reconciler/coordinator looks up by platform string; enterprise builds
	// inject additional adapters here.
	DeploymentAdapters map[string]DeploymentAdapter

	// ExtraRoutes allows external integrations to register additional HTTP routes
	// using the same API instance and path prefix as OSS core routes.
	ExtraRoutes func(api huma.API, pathPrefix string)

	// HTTPServerFactory is an optional function to create a server that adds new API routes.
	HTTPServerFactory HTTPServerFactory

	// OnHTTPServerCreated is an optional callback that receives the created server
	// (potentially extended via HTTPServerFactory).
	OnHTTPServerCreated func(Server)

	// UIHandler is an optional HTTP handler for serving a custom UI at the root path ("/").
	// If provided, this handler will be used instead of the default redirect to docs.
	// API routes will still take precedence over the UI handler.
	UIHandler http.Handler

	// AuthnProvider is an optional authentication provider.
	AuthnProvider auth.AuthnProvider

	// AuthzProvider is an optional authorization provider.
	AuthzProvider auth.AuthzProvider
}

// Server represents the HTTP server and provides access to the Huma API
// and HTTP mux for registering new routes and handlers.
//
// This interface allows external packages to extend the server functionality
// by adding new endpoints without accessing internal implementation details.
type Server interface {
	// HumaAPI returns the Huma API instance, allowing registration of new routes
	// that will appear in the OpenAPI documentation.
	HumaAPI() huma.API

	// Mux returns the HTTP ServeMux, allowing registration of custom HTTP handlers
	Mux() *http.ServeMux

	// Start begins listening for incoming HTTP requests
	Start() error

	// Shutdown gracefully shuts down the server
	Shutdown(ctx context.Context) error
}

// DaemonManager defines the interface for managing the CLI's backend daemon.
// External libraries can implement this to use their own orchestration.
type DaemonManager interface {
	// IsRunning checks if the daemon is currently running
	IsRunning() bool
	// Start starts the daemon and waits until it's ready to serve requests
	Start() error
	// Stop stops the daemon but preserves data volumes
	Stop() error
	// Purge stops the daemon and removes all data volumes
	Purge() error
}

// CLITokenProvider provides tokens for CLI commands.
// External libraries can implement this to support fetching tokens from defined sources
type CLITokenProvider interface {
	// Token returns token for API calls.
	Token(ctx context.Context) (token string, err error)
}

// CLITokenProviderFactory is a function type that creates a CLI token provider.
// The factory optionally receives the root command, which can be used to access command-specific configuration (e.g. flags).
type CLITokenProviderFactory func(root *cobra.Command) (CLITokenProvider, error)

// HTTPServerFactory is a function type that creates a server implementation that
// adds new API routes and handlers.
//
// The factory receives a Server interface and should return a Server after
// registering new routes using base.HumaAPI() or base.Mux().
type HTTPServerFactory func(base Server, store database.Store) Server

// DaemonConfig allows customization of the default daemon manager
type DaemonConfig struct {
	BaseURL string
}

// Response is a generic wrapper for Huma responses
// Usage: Response[HealthBody] instead of HealthOutput
type Response[T any] struct {
	Body T
}

// EmptyResponse represents a simple success response with a message
type EmptyResponse struct {
	Message string `json:"message" doc:"Success message" example:"Operation completed successfully"`
}

// Example usage:
// Instead of:
//   type HealthOutput struct {
//       Body HealthBody
//   }
//
// You could use:
//   type HealthOutput = Response[HealthBody]
//
// Or directly in the handler:
//   func(...) (*Response[HealthBody], error) {
//       return &Response[HealthBody]{
//           Body: HealthBody{...},
//       }, nil
//   }

package types

import (
	"context"
	"errors"
	"net/http"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
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

// ServiceFactory is a function type that creates a service implementation.
// The base service is provided as input, and the factory should return a service
// that implements RegistryService (and optionally additional interfaces).
type ServiceFactory func(base service.RegistryService) service.RegistryService

// DeploymentTarget defines deployment behavior for a provider platform type.
// OSS can provide built-ins while enterprise can register cloud platform handlers.
type DeploymentTarget interface {
	Provider() string
	SupportedResourceTypes() []string
	Deploy(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	Undeploy(ctx context.Context, deployment *models.Deployment) error
}

// AsyncDeploymentTarget extends DeploymentTarget with async job operations.
type AsyncDeploymentTarget interface {
	DeploymentTarget
	GetLogs(ctx context.Context, deployment *models.Deployment) ([]string, error)
	Cancel(ctx context.Context, deployment *models.Deployment) error
}

// Discoverer can be implemented by provider platform handlers that support discovery.
type Discoverer interface {
	Discover(ctx context.Context, providerID string) ([]*models.Deployment, error)
}

// ProviderPlatformAdapter defines provider CRUD behavior for a provider platform type.
type ProviderPlatformAdapter interface {
	Platform() string
	ListProviders(ctx context.Context) ([]*models.Provider, error)
	CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	GetProvider(ctx context.Context, providerID string) (*models.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}

// DeploymentPlatformAdapter defines deployment behavior for a provider platform type.
// This is the intended long-term adapter contract for /v0/deployments dispatch.
type DeploymentPlatformAdapter interface {
	Platform() string
	SupportedResourceTypes() []string
	Deploy(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	Undeploy(ctx context.Context, deployment *models.Deployment) error
	GetLogs(ctx context.Context, deployment *models.Deployment) ([]string, error)
	Cancel(ctx context.Context, deployment *models.Deployment) error
	Discover(ctx context.Context, providerID string) ([]*models.Deployment, error)
}

// DatabaseFactory is a function type that creates a database implementation.
// This allows implementors to run additional migrations and wrap the database.
type DatabaseFactory func(ctx context.Context, databaseURL string, baseDB database.Database, authz auth.Authorizer) (database.Database, error)

// AppOptions contains configuration for the registry app.
// All fields are optional and allow external developers to extend functionality.
//
// This type is defined in pkg/registry and used by both pkg/registry/registry_app.go
// and internal/registry/registry_app.go to avoid circular dependencies.
type AppOptions struct {
	// DatabaseFactory is an optional function to create a database that adds new functionality.
	// The factory receives the base database and can run additional migrations.
	// If nil, uses the default PostgreSQL database.
	DatabaseFactory DatabaseFactory

	// ServiceFactory is an optional function to create a service that adds new functionality.
	// The factory receives the base service and should return an extended service.
	ServiceFactory ServiceFactory

	// DeploymentTargets registers additional deployment targets.
	// OSS provides built-in local runtimes; enterprise can register cloud providers.
	DeploymentTargets map[string]DeploymentTarget

	// ProviderPlatforms registers adapters for provider CRUD by provider platform type.
	ProviderPlatforms map[string]ProviderPlatformAdapter

	// DeploymentPlatforms registers adapters for deployment lifecycle by provider platform type.
	DeploymentPlatforms map[string]DeploymentPlatformAdapter

	// ExtraRoutes allows external integrations to register additional HTTP routes
	// using the same API instance and path prefix as OSS core routes.
	ExtraRoutes func(api huma.API, pathPrefix string)

	// OnServiceCreated is an optional callback that receives the created service
	// (potentially extended via ServiceFactory).
	OnServiceCreated func(service.RegistryService)

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
	// Start starts the daemon, blocking until it's ready
	Start() error
}

// CLIAuthnProvider provides authentication for CLI commands.
// External libraries can implement this to support different auth mechanisms
type CLIAuthnProvider interface {
	// Authenticate returns credentials for API calls.
	Authenticate(ctx context.Context) (token string, err error)
}

// CLIAuthnProviderFactory is a function type that creates a CLI authentication provider.
// The factory optionally receives the root command, which can be used to access command-specific configuration (e.g. flags).
type CLIAuthnProviderFactory func(root *cobra.Command) (CLIAuthnProvider, error)

// HTTPServerFactory is a function type that creates a server implementation that
// adds new API routes and handlers.
//
// The factory receives a Server interface and should return a Server after
// registering new routes using base.HumaAPI() or base.Mux().
type HTTPServerFactory func(base Server, db database.Database) Server

// DaemonConfig allows customization of the default daemon manager
type DaemonConfig struct {
	ProjectName    string // docker compose project name (default: "agentregistry")
	ContainerName  string // container name to check for running state (default: "agentregistry-server")
	ComposeYAML    string // docker-compose.yml content (default: embedded)
	DockerRegistry string // image registry (default: version.DockerRegistry)
	Version        string // image version (default: version.Version)
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

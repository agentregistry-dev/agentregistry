package api

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/cors"

	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

//go:embed all:ui/dist
var embeddedUI embed.FS

// createUIHandler creates an HTTP handler for serving the embedded UI files
func createUIHandler() (http.Handler, error) {
	// Extract the ui/dist subdirectory from the embedded filesystem
	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		return nil, err
	}

	// Create a file server for the UI
	return http.FileServer(http.FS(uiFS)), nil
}

// TrailingSlashMiddleware redirects requests with trailing slashes to their canonical form
// Only applies to API routes, not UI routes
func TrailingSlashMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only apply trailing slash logic to API routes
		isAPIRoute := strings.HasPrefix(r.URL.Path, "/v0/") ||
			strings.HasPrefix(r.URL.Path, "/v0.1/") ||
			r.URL.Path == "/health" ||
			r.URL.Path == "/ping" ||
			r.URL.Path == "/metrics" ||
			strings.HasPrefix(r.URL.Path, "/docs")

		// Only redirect if it's an API route and ends with a "/"
		if isAPIRoute && r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
			// Create a copy of the URL and remove the trailing slash
			newURL := *r.URL
			newURL.Path = strings.TrimSuffix(r.URL.Path, "/")

			// Use 308 Permanent Redirect to preserve the request method
			http.Redirect(w, r, newURL.String(), http.StatusPermanentRedirect)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Server represents the HTTP server
type Server struct {
	config   *config.Config
	registry service.RegistryService
	humaAPI  huma.API
	mux      *http.ServeMux
	server   *http.Server
}

// HumaAPI returns the Huma API instance, allowing registration of new routes
func (s *Server) HumaAPI() huma.API {
	return s.humaAPI
}

// Mux returns the HTTP ServeMux, allowing registration of custom HTTP handlers
func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, registryService service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody, customUIHandler http.Handler) *Server {
	// Create HTTP mux and Huma API
	mux := http.NewServeMux()

	var uiHandler http.Handler

	if customUIHandler != nil {
		uiHandler = customUIHandler
	} else {
		var err error
		uiHandler, err = createUIHandler()
		if err != nil {
			log.Printf("Warning: Failed to create UI handler: %v. UI will not be served.", err)
			uiHandler = nil
		} else {
			log.Println("UI handler initialized - web interface will be available")
		}
	}

	api := router.NewHumaAPI(cfg, registryService, mux, metrics, versionInfo, uiHandler)

	// Configure CORS with permissive settings for public API
	corsHandler := cors.New(cors.Options{
		AllowedOrigins: []string{"*"},
		AllowedMethods: []string{
			http.MethodGet,
			http.MethodPost,
			http.MethodPut,
			http.MethodDelete,
			http.MethodOptions,
		},
		AllowedHeaders:   []string{"*"},
		ExposedHeaders:   []string{"Content-Type", "Content-Length"},
		AllowCredentials: false, // Must be false when AllowedOrigins is "*"
		MaxAge:           86400, // 24 hours
	})

	// Wrap the mux with middleware stack
	// Order: TrailingSlash -> CORS -> Mux
	handler := TrailingSlashMiddleware(corsHandler.Handler(mux))

	server := &Server{
		config:   cfg,
		registry: registryService,
		humaAPI:  api,
		mux:      mux,
		server: &http.Server{
			Addr:              cfg.ServerAddress,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}

	return server
}

// Start begins listening for incoming HTTP requests
func (s *Server) Start() error {
	log.Printf("HTTP server starting on %s", s.config.ServerAddress)
	log.Printf("Web UI available at http://localhost%s/", s.config.ServerAddress)
	log.Printf("API documentation at http://localhost%s/docs", s.config.ServerAddress)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

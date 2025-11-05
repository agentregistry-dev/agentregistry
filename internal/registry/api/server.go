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
// Excludes /ui paths since the UI needs to handle its own routing
func TrailingSlashMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip trailing slash handling for UI paths
		if strings.HasPrefix(r.URL.Path, "/ui") {
			next.ServeHTTP(w, r)
			return
		}

		// Only redirect if the path is not "/" and ends with a "/"
		if r.URL.Path != "/" && strings.HasSuffix(r.URL.Path, "/") {
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
	server   *http.Server
}

// NewServer creates a new HTTP server
func NewServer(cfg *config.Config, registryService service.RegistryService, metrics *telemetry.Metrics, versionInfo *v0.VersionBody) *Server {
	// Create HTTP mux and Huma API
	mux := http.NewServeMux()

	// Serve embedded UI
	mux.Handle("/", http.FileServer(http.FS(embeddedUI)))

	// Create UI handler
	uiHandler, err := createUIHandler()
	if err != nil {
		log.Printf("Warning: Failed to create UI handler: %v. UI will not be served.", err)
		uiHandler = nil
	} else {
		log.Println("UI handler initialized - web interface will be available")
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
	log.Printf("Web UI available at http://localhost%s/ui", s.config.ServerAddress)
	log.Printf("API documentation at http://localhost%s/docs", s.config.ServerAddress)
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

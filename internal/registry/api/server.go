package api

import (
	"context"
	"embed"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/rs/cors"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

//go:embed all:ui/dist
var embeddedUI embed.FS

// createUIHandler creates an HTTP handler for serving the embedded UI files.
// It uses a similar approach to NGINX's try_files.
// It handles Next.js static export routing by trying the exact path, then
// <path>.html (for page routes like /deployed -> deployed.html), then falling
// back to index.html for client-side SPA routing.
func createUIHandler() (http.Handler, error) {
	// Extract the ui/dist subdirectory from the embedded filesystem
	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		return nil, err
	}

	fileServer := http.FileServer(http.FS(uiFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")

		// Try the exact path as a file first (e.g. _next/static/..., index.html)
		if f, err := uiFS.Open(path); err == nil {
			info, err := f.Stat()
			f.Close()
			// Only serve directly if it's a regular file, not a directory
			if err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// Try <path>.html (Next.js static export puts /deployed -> deployed.html)
		if htmlPath := path + ".html"; path != "" {
			if f, err := uiFS.Open(htmlPath); err == nil {
				f.Close()
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/" + htmlPath
				fileServer.ServeHTTP(w, r2)
				return
			}
		}

		// Fall back to index.html for SPA client-side routing.
		// Use `ServeContent` to avoid http.FileServer's built-in
		// redirect from /index.html -> / which causes an infinite redirect loop.
		// Ref: https://pkg.go.dev/net/http#ServeFile
		indexFile, err := uiFS.Open("index.html")
		if err != nil {
			http.Error(w, "index.html not found", http.StatusNotFound)
			return
		}
		defer indexFile.Close()

		stat, err := indexFile.Stat()
		if err != nil {
			http.Error(w, "failed to stat index.html", http.StatusInternalServerError)
			return
		}

		http.ServeContent(w, r, "index.html", stat.ModTime(), indexFile.(io.ReadSeeker))
	}), nil
}

// TrailingSlashMiddleware redirects requests with trailing slashes to their canonical form
// Only applies to API routes, not UI routes
func TrailingSlashMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only apply trailing slash logic to API routes
		isAPIRoute := strings.HasPrefix(r.URL.Path, "/v0/") ||
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
// Note: AuthZ is handled at the DB/service layer, not at the API layer.
func NewServer(cfg *config.Config, registryService service.RegistryService, metrics *telemetry.Metrics, versionInfo *apitypes.VersionBody, customUIHandler http.Handler, authnProvider auth.AuthnProvider, routeOpts *router.RouteOptions) *Server {
	// Create HTTP mux and Huma API
	mux := http.NewServeMux()

	var uiHandler http.Handler

	if customUIHandler != nil {
		uiHandler = customUIHandler
	} else {
		var err error
		uiHandler, err = createUIHandler()
		if err != nil {
			slog.Warn("failed to create UI handler; UI will not be served", "error", err)
			uiHandler = nil
		} else {
			slog.Info("UI handler initialized; web interface will be available")
		}
	}

	api := router.NewHumaAPI(cfg, registryService, mux, metrics, versionInfo, uiHandler, authnProvider, routeOpts)

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
	slog.Info("HTTP server starting", "address", s.config.ServerAddress)
	slog.Info("web UI available", "url", fmt.Sprintf("http://localhost%s/", s.config.ServerAddress))
	slog.Info("API documentation available", "url", fmt.Sprintf("http://localhost%s/docs", s.config.ServerAddress))
	return s.server.ListenAndServe()
}

// Shutdown gracefully shuts down the server
func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

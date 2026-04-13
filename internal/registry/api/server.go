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
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
)

//go:embed all:ui/dist
var embeddedUI embed.FS

// newUIHandler builds the try-files HTTP handler from any fs.FS.
// Separated from createUIHandler to allow unit testing with a fake filesystem.
//
// Routing mirrors NGINX's try_files $uri $uri.html /index.html:
//  1. Exact file match (e.g. _next/static/chunk.abc123.js)
//  2. <path>.html match (Next.js static export: /deployed -> deployed.html)
//  3. Missing path with a file extension -> 404 (avoids serving index.html for broken asset refs)
//  4. Anything else -> index.html (SPA client-side routing fallback)
func newUIHandler(uiFS fs.FS) (http.Handler, error) {
	fileServer := http.FileServer(http.FS(uiFS))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize trailing slash so /foo and /foo/ resolve consistently.
		// TrailingSlashMiddleware only covers API routes, not UI routes.
		path := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/"), "/")

		// 1. Try the exact path as a file (not a directory).
		if f, err := uiFS.Open(path); err == nil {
			info, err := f.Stat()
			f.Close()
			if err == nil && !info.IsDir() {
				fileServer.ServeHTTP(w, r)
				return
			}
		}

		// 2. Try <path>.html (Next.js static export: /deployed -> deployed.html).
		if path != "" {
			if f, err := uiFS.Open(path + ".html"); err == nil {
				f.Close()
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/" + path + ".html"
				fileServer.ServeHTTP(w, r2)
				return
			}
		}

		// 3. If the last path segment has a file extension, it's a missing asset — 404.
		if strings.Contains(path[strings.LastIndex(path, "/")+1:], ".") {
			http.NotFound(w, r)
			return
		}

		// 4. SPA fallback: serve index.html.
		// Use ServeContent directly to avoid http.FileServer's built-in redirect
		// of any path ending in "/index.html" back to "/", which would loop.
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

		rs, ok := indexFile.(io.ReadSeeker)
		if !ok {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		http.ServeContent(w, r, "index.html", stat.ModTime(), rs)
	}), nil
}

// createUIHandler creates an HTTP handler for serving the embedded UI files.
func createUIHandler() (http.Handler, error) {
	uiFS, err := fs.Sub(embeddedUI, "ui/dist")
	if err != nil {
		return nil, err
	}
	return newUIHandler(uiFS)
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

type Server struct {
	config  *config.Config
	humaAPI huma.API
	mux     *http.ServeMux
	server  *http.Server
}

func (s *Server) HumaAPI() huma.API {
	return s.humaAPI
}

func (s *Server) Mux() *http.ServeMux {
	return s.mux
}

// AuthZ is handled at the DB/service layer, not at the API layer.
func NewServer(
	cfg *config.Config,
	svcs router.RegistryServices,
	metrics *telemetry.Metrics,
	versionInfo *apitypes.VersionBody,
	customUIHandler http.Handler,
	authnProvider auth.AuthnProvider,
	routeOpts *router.RouteOptions,
) *Server {
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

	api := router.NewHumaAPI(cfg, svcs, mux, metrics, versionInfo, uiHandler, authnProvider, routeOpts)

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
		config:  cfg,
		humaAPI: api,
		mux:     mux,
		server: &http.Server{
			Addr:              cfg.ServerAddress,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		},
	}

	return server
}

func (s *Server) Start() error {
	slog.Info("HTTP server starting", "address", s.config.ServerAddress)
	slog.Info("web UI available", "url", fmt.Sprintf("http://localhost%s/", s.config.ServerAddress))
	slog.Info("API documentation available", "url", fmt.Sprintf("http://localhost%s/docs", s.config.ServerAddress))
	return s.server.ListenAndServe()
}

func (s *Server) Shutdown(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}

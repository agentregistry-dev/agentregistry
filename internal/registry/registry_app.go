package registry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpregistry "github.com/agentregistry-dev/agentregistry/internal/mcp/registryserver"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/resource"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/kubernetes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/local"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	pkgimporter "github.com/agentregistry-dev/agentregistry/pkg/importer"
	osvscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/osv"
	scorecardscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/scorecard"
	"github.com/agentregistry-dev/agentregistry/pkg/logging"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func App(ctx context.Context, opts ...types.AppOptions) error {
	var options types.AppOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	cfg := config.NewConfig()
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Create a context with timeout for PostgreSQL connection
	dbCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	setupLogging(cfg.LogLevel)

	// Build auth providers from options (before database creation)
	// Only create jwtManager if JWT is configured
	var jwtManager *auth.JWTManager
	if cfg.JWTPrivateKey != "" {
		jwtManager = auth.NewJWTManager(cfg)
	}

	// Resolve authn provider: use provided, or default to JWT-based if configured
	authnProvider := options.AuthnProvider
	if authnProvider == nil && jwtManager != nil {
		authnProvider = jwtManager
	}

	// Resolve authz provider: use provided, or default to public authz
	authzProvider := options.AuthzProvider
	if authzProvider == nil {
		slog.Info("using public authz provider")
		authzProvider = auth.NewPublicAuthzProvider(jwtManager)
	}
	authz := auth.Authorizer{Authz: authzProvider}

	// Database selection: use DATABASE_URL="noop" only when you provide the database
	// entirely via AppOptions.DatabaseFactory (e.g. in-memory or custom backend) and
	// do not want a real PostgreSQL connection. In that case DatabaseFactory is required.
	// For normal deployments, set DATABASE_URL to a real Postgres connection string.
	var db database.Store
	if cfg.DatabaseURL == "noop" { //nolint:nestif
		if options.DatabaseFactory == nil {
			return fmt.Errorf("DATABASE_URL=noop requires DatabaseFactory to be set in AppOptions")
		}
		slog.Info("using DatabaseFactory to create database", "mode", "noop")
		var err error
		db, err = options.DatabaseFactory(ctx, "", nil, authz)
		if err != nil {
			return fmt.Errorf("failed to create database via factory: %w", err)
		}
	} else {
		baseDB, err := internaldb.NewPostgreSQL(dbCtx, cfg.DatabaseURL, authz)
		if err != nil {
			return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
		}

		// Allow implementors to wrap the database and run additional migrations
		db = baseDB
		if options.DatabaseFactory != nil {
			db, err = options.DatabaseFactory(ctx, cfg.DatabaseURL, baseDB, authz)
			if err != nil {
				if err := baseDB.Close(); err != nil {
					slog.Error("error closing base database connection", "error", err)
				}
				return fmt.Errorf("failed to create extended database: %w", err)
			}
		}
	}

	// Store the database instance for later cleanup
	defer func() {
		if err := db.Close(); err != nil {
			slog.Error("error closing database connection", "error", err)
		} else {
			slog.Info("database connection closed successfully")
		}
	}()

	// v1alpha1 DeploymentAdapter map consumed by the coordinator below.
	// Built OSS-side from the local + kubernetes ports; enterprise extends
	// via AppOptions.DeploymentAdapters.
	v1alpha1Adapters := map[string]types.DeploymentAdapter{
		"local":      local.NewLocalDeploymentAdapter(cfg.RuntimeDir, cfg.AgentGatewayPort),
		"kubernetes": kubernetes.NewKubernetesDeploymentAdapter(),
	}
	maps.Copy(v1alpha1Adapters, options.DeploymentAdapters)
	pool := db.Pool()
	v1alpha1Stores, v1alpha1Importer := buildV1Alpha1Bundle(pool)
	startBuiltinSeedImport(cfg, pool)
	startSeedFromImport(cfg, v1alpha1Importer)

	slog.Info("starting agentregistry", "version", version.Version, "commit", version.GitCommit)

	// Prepare version information
	versionInfo := &arv0.VersionBody{
		Version:   version.Version,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildDate,
	}

	shutdownTelemetry, metrics, err := telemetry.InitMetrics(cfg.Version)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %v", err)
	}

	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			slog.Error("failed to shutdown telemetry", "error", err)
		}
	}()

	routeOpts := buildRouteOptions(cfg, options, authz, v1alpha1Stores, v1alpha1Importer, v1alpha1Adapters)

	// Initialize HTTP server
	baseServer := api.NewServer(cfg, metrics, versionInfo, options.UIHandler, authnProvider, routeOpts)

	var server types.Server
	if options.HTTPServerFactory != nil {
		server = options.HTTPServerFactory(baseServer, db)
	} else {
		server = baseServer
	}

	if options.OnHTTPServerCreated != nil {
		options.OnHTTPServerCreated(server)
	}

	var mcpHTTPServer *http.Server
	if cfg.MCPPort > 0 && v1alpha1Stores != nil {
		mcpServer := mcpregistry.NewServer(v1alpha1Stores)

		var handler http.Handler = mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return mcpServer
		}, &mcp.StreamableHTTPOptions{})

		// Set up authentication middleware if one is configured
		if authnProvider != nil {
			handler = mcpAuthnMiddleware(authnProvider)(handler)
		}

		addr := ":" + strconv.Itoa(int(cfg.MCPPort))
		mcpHTTPServer = &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			slog.Info("MCP HTTP server starting", "address", addr)
			if err := mcpHTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("failed to start MCP server", "error", err)
				os.Exit(1)
			}
		}()
	}

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to start server", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	slog.Info("shutting down server")

	// Create context with timeout for shutdown
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(sctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}
	if mcpHTTPServer != nil {
		if err := mcpHTTPServer.Shutdown(sctx); err != nil {
			slog.Error("MCP server forced to shutdown", "error", err)
		}
	}

	slog.Info("server exiting")
	return nil
}

func buildV1Alpha1Bundle(pool *pgxpool.Pool) (map[string]*internaldb.Store, *pkgimporter.Importer) {
	if pool == nil {
		slog.Info("v1alpha1 routes disabled: database Pool() is nil (likely noop/DatabaseFactory)")
		return nil, nil
	}

	stores := internaldb.NewV1Alpha1Stores(pool)

	// GITHUB_TOKEN (when set in env) authenticates scanner fetches
	// against GitHub's contents + repo API to raise the 60 req/hr
	// unauthenticated limit.
	githubToken := os.Getenv("GITHUB_TOKEN")
	imp, err := pkgimporter.New(pkgimporter.Config{
		Stores:   stores,
		Findings: pkgimporter.NewFindingsStore(pool),
		Scanners: []pkgimporter.Scanner{
			osvscanner.New(osvscanner.Config{GitHubToken: githubToken}),
			scorecardscanner.New(scorecardscanner.Config{GitHubToken: githubToken}),
		},
		Resolver:          internaldb.NewV1Alpha1Resolver(stores),
		RegistryValidator: registries.Dispatcher,
		UniqueRemoteURLs:  internaldb.NewV1Alpha1UniqueRemoteURLsChecker(stores),
	})
	if err != nil {
		slog.Warn("failed to construct v1alpha1 importer; HTTP import + seed-from disabled for this path", "error", err)
		slog.Info("v1alpha1 routes enabled")
		return stores, nil
	}

	slog.Info("v1alpha1 routes enabled")
	return stores, imp
}

func startBuiltinSeedImport(cfg *config.Config, pool *pgxpool.Pool) {
	// Import builtin seed data unless disabled. Writes to v1alpha1.*
	// tables via the generic Store. Skipped when the underlying DB
	// returns a nil pool (noop/test backends) — seeding is decorative
	// for those anyway.
	if cfg.DisableBuiltinSeed {
		return
	}
	if pool == nil {
		slog.Info("builtin seed skipped: database Pool() is nil")
		return
	}

	slog.Info("importing builtin seed data in the background")
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		ctx = auth.WithSystemContext(ctx)
		if err := seed.ImportBuiltinSeedDataV1Alpha1(ctx, pool); err != nil {
			slog.Error("failed to import builtin seed data (v1alpha1)", "error", err)
		}
	}()
}

func startSeedFromImport(cfg *config.Config, v1alpha1Importer *pkgimporter.Importer) {
	// Import seed data if seed source is provided. Requires the
	// v1alpha1 Importer; backends without Pool() support can't seed
	// from disk in the new model.
	if cfg.SeedFrom == "" {
		return
	}
	if v1alpha1Importer == nil {
		slog.Warn("--seed-from requested but v1alpha1 importer unavailable; skipping", "seed_from", cfg.SeedFrom)
		return
	}

	slog.Info("importing data in the background", "seed_from", cfg.SeedFrom)
	go runSeedFromImport(cfg, v1alpha1Importer)
}

func buildRouteOptions(
	cfg *config.Config,
	options types.AppOptions,
	authz auth.Authorizer,
	stores map[string]*internaldb.Store,
	importer *pkgimporter.Importer,
	adapters map[string]types.DeploymentAdapter,
) *router.RouteOptions {
	routeOpts := &router.RouteOptions{
		ExtraRoutes:      options.ExtraRoutes,
		Authz:            authz,
		V1Alpha1Stores:   stores,
		V1Alpha1Importer: importer,
	}

	if stores != nil {
		routeOpts.V1Alpha1DeploymentCoordinator = deploymentsvc.NewV1Alpha1Coordinator(deploymentsvc.V1Alpha1Dependencies{
			Stores:   stores,
			Adapters: adapters,
			Getter:   internaldb.NewV1Alpha1Getter(stores),
		})
	}

	// Embeddings pipeline — Provider + Indexer + jobs.Manager + the
	// `?semantic=<q>` query-embedding func threaded through to the
	// generic resource handler. Wired only when both v1alpha1 Stores
	// exist (pgvector schema is a prerequisite) and
	// AGENT_REGISTRY_EMBEDDINGS_ENABLED=true in config.
	if stores != nil && cfg.Embeddings.Enabled {
		wireEmbeddings(cfg, stores, routeOpts)
	}

	return routeOpts
}

// mcpAuthnMiddleware creates a middleware that uses the AuthnProvider to authenticate requests and add to session context.
// this session context is used by the db + authz provider to check permissions.
func mcpAuthnMiddleware(authn auth.AuthnProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// authenticate using the configured provider
			session, err := authn.Authenticate(ctx, r.Header.Get, r.URL.Query())
			if err == nil && session != nil {
				ctx = auth.AuthSessionTo(ctx, session)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// setupLogging configures the global slog logger
func setupLogging(levelStr string) {
	logging.SetupDefault()

	if levelStr == "" {
		levelStr = "info"
	}
	level, err := logging.ParseLevel(levelStr)
	if err != nil {
		slog.Error("failed to parse log level, defaulting to info", "error", err)
		level = slog.LevelInfo
	}
	// set all loggers to the specified level
	logging.Reset(level)
}

// runSeedFromImport drives the cfg.SeedFrom import in the background
// via the v1alpha1 Importer. Caller guarantees v1alpha1Importer != nil.
func runSeedFromImport(cfg *config.Config, v1alpha1Importer *pkgimporter.Importer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = auth.WithSystemContext(ctx)

	results, err := v1alpha1Importer.Import(ctx, pkgimporter.Options{
		Path:   cfg.SeedFrom,
		Enrich: cfg.EnrichServerData,
	})
	if err != nil {
		slog.Error("failed to import seed data (v1alpha1)", "error", err)
		return
	}
	var failed int
	for _, r := range results {
		if r.Status == pkgimporter.ImportStatusFailed {
			failed++
			slog.Warn("v1alpha1 import failed for document",
				"source", r.Source, "kind", r.Kind,
				"name", r.Name, "error", r.Error)
		}
	}
	slog.Info("v1alpha1 import complete",
		"seed_from", cfg.SeedFrom,
		"total", len(results), "failed", failed)
}

// makeSemanticSearchFunc wraps an embeddings.Provider into the
// resource.SemanticSearchFunc shape the list handler expects. Shared
// by the GET `/v0/{plural}?semantic=<q>` path across all kinds —
// callers don't care how the vector was produced, just that the
// provider speaks the same model the indexer used.
func makeSemanticSearchFunc(provider embeddings.Provider, dimensions int) resource.SemanticSearchFunc {
	return func(ctx context.Context, query string) ([]float32, error) {
		emb, err := embeddings.GenerateSemanticEmbedding(ctx, provider, query, dimensions)
		if err != nil {
			return nil, err
		}
		return emb.Vector, nil
	}
}

// wireEmbeddings constructs the Provider + Indexer + jobs.Manager +
// semantic-search func and plants them on routeOpts. Split from App
// for readability — each of the three construction steps has an
// error-log + bail-out path, making the inline code deeply nested.
// Any construction failure leaves the corresponding routeOpts fields
// nil so the endpoints + list-handler `?semantic=` return 4xx/503.
func wireEmbeddings(cfg *config.Config, stores map[string]*internaldb.Store, routeOpts *router.RouteOptions) {
	provider, err := embeddings.Factory(&cfg.Embeddings, nil)
	if err != nil {
		slog.Warn("embeddings enabled but provider factory failed; semantic search + indexing disabled",
			"error", err)
		return
	}

	bindings, err := embeddings.DefaultBindings(stores)
	if err != nil {
		slog.Warn("embeddings enabled but DefaultBindings failed", "error", err)
		return
	}

	idx, err := embeddings.NewIndexer(embeddings.IndexerConfig{
		Bindings:   bindings,
		Provider:   provider,
		Dimensions: cfg.Embeddings.Dimensions,
	})
	if err != nil {
		slog.Warn("embeddings enabled but Indexer construction failed", "error", err)
		return
	}

	routeOpts.V1Alpha1Indexer = idx
	routeOpts.V1Alpha1JobManager = jobs.NewManager()
	routeOpts.V1Alpha1SemanticSearch = makeSemanticSearchFunc(provider, cfg.Embeddings.Dimensions)
	slog.Info("embeddings indexer + semantic search enabled",
		"provider", cfg.Embeddings.Provider,
		"model", cfg.Embeddings.Model)
}

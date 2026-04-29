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
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/crud"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/kubernetes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/local"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	pkgimporter "github.com/agentregistry-dev/agentregistry/pkg/importer"
	osvscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/osv"
	scorecardscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/scorecard"
	"github.com/agentregistry-dev/agentregistry/pkg/logging"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/resource"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
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

	db, err := openDatabase(ctx, dbCtx, cfg, options, authz)
	if err != nil {
		return err
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
	deploymentAdapters := map[string]types.DeploymentAdapter{
		"local":      local.NewLocalDeploymentAdapter(cfg.RuntimeDir, cfg.AgentGatewayPort),
		"kubernetes": kubernetes.NewKubernetesDeploymentAdapter(),
	}
	maps.Copy(deploymentAdapters, options.DeploymentAdapters)
	pool := db.Pool()
	registryValidator := options.RegistryValidator
	if registryValidator == nil {
		registryValidator = registries.Dispatcher
	}
	stores, importer := buildStoresAndImporter(pool, registryValidator)
	startBuiltinSeedImport(cfg, pool)
	startSeedFromImport(cfg, importer)

	slog.Info("starting agentregistry", "version", version.Version, "commit", version.GitCommit)

	// Prepare version information
	versionInfo := &arv0.VersionBody{
		Version:   version.Version,
		GitCommit: version.GitCommit,
		BuildTime: version.BuildDate,
	}

	shutdownTelemetry, metrics, err := telemetry.InitMetrics(cfg.Version)
	if err != nil {
		return fmt.Errorf("failed to initialize metrics: %w", err)
	}

	defer func() {
		if err := shutdownTelemetry(context.Background()); err != nil {
			slog.Error("failed to shutdown telemetry", "error", err)
		}
	}()

	routeOpts := buildRouteOptions(cfg, options, authz, stores, importer, deploymentAdapters)

	// Initialize HTTP server
	baseServer, err := api.NewServer(cfg, metrics, versionInfo, options.UIHandler, authnProvider, routeOpts)
	if err != nil {
		return fmt.Errorf("failed to initialize HTTP server: %w", err)
	}

	var server types.Server
	if options.HTTPServerFactory != nil {
		server = options.HTTPServerFactory(baseServer, db)
	} else {
		server = baseServer
	}

	if options.OnHTTPServerCreated != nil {
		options.OnHTTPServerCreated(server)
	}

	mcpHTTPServer := startMCPServer(cfg, stores, authnProvider)

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

func buildStoresAndImporter(pool *pgxpool.Pool, registryValidator v1alpha1.RegistryValidatorFunc) (map[string]*v1alpha1store.Store, *pkgimporter.Importer) {
	stores := v1alpha1store.NewStores(pool)

	// pool == nil is the noop/DatabaseFactory path used by gen-openapi
	// and the release-openapi make target. Routes still register so the
	// generated OpenAPI captures every endpoint, but actual queries
	// would crash on the nil pool — that's fine because the noop path
	// never serves real traffic.
	if pool == nil {
		slog.Info("v1alpha1 routes registered against nil pool: query path will panic if exercised (likely noop/DatabaseFactory)")
		return stores, nil
	}

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
		Resolver:          internaldb.NewResolver(stores),
		RegistryValidator: registryValidator,
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
		if err := seed.ImportBuiltinSeedData(ctx, pool); err != nil {
			slog.Error("failed to import builtin seed data (v1alpha1)", "error", err)
		}
	}()
}

func startSeedFromImport(cfg *config.Config, importer *pkgimporter.Importer) {
	// Import seed data if seed source is provided. Requires the
	// v1alpha1 Importer; backends without Pool() support can't seed
	// from disk in the new model.
	if cfg.SeedFrom == "" {
		return
	}
	if importer == nil {
		slog.Warn("--seed-from requested but v1alpha1 importer unavailable; skipping", "seed_from", cfg.SeedFrom)
		return
	}

	slog.Info("importing data in the background", "seed_from", cfg.SeedFrom)
	go runSeedFromImport(cfg, importer)
}

func buildRouteOptions(
	cfg *config.Config,
	options types.AppOptions,
	authz auth.Authorizer,
	stores map[string]*v1alpha1store.Store,
	importer *pkgimporter.Importer,
	adapters map[string]types.DeploymentAdapter,
) *router.RouteOptions {
	routeOpts := &router.RouteOptions{
		ExtraRoutes:       options.ExtraRoutes,
		Authz:             authz,
		Stores:            stores,
		Importer:          importer,
		PerKindHooks:      crudPerKindHooks(options),
		RegistryValidator: options.RegistryValidator,
	}

	if stores != nil {
		routeOpts.DeploymentCoordinator = deploymentsvc.NewCoordinator(deploymentsvc.Dependencies{
			Stores:   stores,
			Adapters: adapters,
			Getter:   internaldb.NewGetter(stores),
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

// crudPerKindHooks adapts the AppOptions per-kind authorizer +
// list-filter maps (which use the public pkg/types signatures) into
// the internal crud.PerKindHooks struct (which uses the
// resource.AuthorizeInput type the generic resource handler
// dispatches on). Field-for-field copy across the two
// AuthorizeInput-shaped structs.
func crudPerKindHooks(options types.AppOptions) crud.PerKindHooks {
	hooks := crud.PerKindHooks{}
	if len(options.Authorizers) > 0 {
		hooks.Authorizers = make(map[string]func(ctx context.Context, in resource.AuthorizeInput) error, len(options.Authorizers))
		for kind, fn := range options.Authorizers {
			f := fn
			hooks.Authorizers[kind] = func(ctx context.Context, in resource.AuthorizeInput) error {
				return f(ctx, types.AuthorizeInput{
					Verb: in.Verb, Kind: in.Kind, Namespace: in.Namespace,
					Name: in.Name, Version: in.Version,
				})
			}
		}
	}
	if len(options.ListFilters) > 0 {
		hooks.ListFilters = make(map[string]func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error), len(options.ListFilters))
		for kind, fn := range options.ListFilters {
			f := fn
			hooks.ListFilters[kind] = func(ctx context.Context, in resource.AuthorizeInput) (string, []any, error) {
				return f(ctx, types.AuthorizeInput{
					Verb: in.Verb, Kind: in.Kind, Namespace: in.Namespace,
					Name: in.Name, Version: in.Version,
				})
			}
		}
	}
	// PostUpserts / PostDeletes are already (ctx, v1alpha1.Object) →
	// error so they pass through verbatim — no adapter needed.
	if len(options.PostUpserts) > 0 {
		hooks.PostUpserts = make(map[string]func(ctx context.Context, obj v1alpha1.Object) error, len(options.PostUpserts))
		for kind, fn := range options.PostUpserts {
			hooks.PostUpserts[kind] = fn
		}
	}
	if len(options.PostDeletes) > 0 {
		hooks.PostDeletes = make(map[string]func(ctx context.Context, obj v1alpha1.Object) error, len(options.PostDeletes))
		for kind, fn := range options.PostDeletes {
			hooks.PostDeletes[kind] = fn
		}
	}
	// ProviderPlatforms map dispatches the KindProvider PostUpsert /
	// PostDelete by Spec.Platform → adapter. A Provider whose platform
	// has no registered adapter is a no-op (matches the OSS default
	// where AppOptions.ProviderPlatforms is empty). When both an
	// explicit PostUpserts[KindProvider] and ProviderPlatforms
	// are present, the dispatcher chains: caller hook first, then the
	// platform adapter.
	if len(options.ProviderPlatforms) > 0 {
		adapters := make(map[string]types.ProviderPlatformAdapter, len(options.ProviderPlatforms))
		maps.Copy(adapters, options.ProviderPlatforms)
		if hooks.PostUpserts == nil {
			hooks.PostUpserts = map[string]func(ctx context.Context, obj v1alpha1.Object) error{}
		}
		if hooks.PostDeletes == nil {
			hooks.PostDeletes = map[string]func(ctx context.Context, obj v1alpha1.Object) error{}
		}
		hooks.PostUpserts[v1alpha1.KindProvider] = providerPlatformDispatcher(
			hooks.PostUpserts[v1alpha1.KindProvider], adapters,
			func(ctx context.Context, p *v1alpha1.Provider, a types.ProviderPlatformAdapter) error {
				return a.ApplyProvider(ctx, p)
			},
		)
		hooks.PostDeletes[v1alpha1.KindProvider] = providerPlatformDispatcher(
			hooks.PostDeletes[v1alpha1.KindProvider], adapters,
			func(ctx context.Context, p *v1alpha1.Provider, a types.ProviderPlatformAdapter) error {
				return a.RemoveProvider(ctx, p.Metadata.Name)
			},
		)
	}
	return hooks
}

// providerPlatformDispatcher wraps a (kind=Provider) hook so the caller
// hook (if any) runs first, then dispatches to the per-platform adapter
// matching provider.Spec.Platform. A Provider with no registered
// adapter is a no-op so the hook stays safe for partial wiring.
func providerPlatformDispatcher(
	caller func(ctx context.Context, obj v1alpha1.Object) error,
	adapters map[string]types.ProviderPlatformAdapter,
	dispatch func(ctx context.Context, p *v1alpha1.Provider, a types.ProviderPlatformAdapter) error,
) func(ctx context.Context, obj v1alpha1.Object) error {
	return func(ctx context.Context, obj v1alpha1.Object) error {
		if caller != nil {
			if err := caller(ctx, obj); err != nil {
				return err
			}
		}
		provider, ok := obj.(*v1alpha1.Provider)
		if !ok || provider == nil {
			return nil
		}
		adapter, ok := adapters[provider.Spec.Platform]
		if !ok {
			return nil
		}
		return dispatch(ctx, provider, adapter)
	}
}

// openDatabase selects and constructs the base Store (plus any
// DatabaseFactory wrap) and returns it. Two paths:
//   - DATABASE_URL="noop" requires options.DatabaseFactory to supply the
//     Store entirely (e.g. in-memory or custom backend). Used by tests
//     and noop runs.
//   - Otherwise connect to PostgreSQL; if a DatabaseFactory is set, it
//     wraps the base pool so implementors can run additional migrations
//     and layer authz/caching on top.
//
// On factory failure the base pool is closed before returning the wrap
// error so we don't leak connections into the caller's error path.
func openDatabase(
	appCtx, dbCtx context.Context,
	cfg *config.Config,
	options types.AppOptions,
	authz auth.Authorizer,
) (pkgdb.Store, error) {
	if cfg.DatabaseURL == "noop" {
		if options.DatabaseFactory == nil {
			return nil, fmt.Errorf("DATABASE_URL=noop requires DatabaseFactory to be set in AppOptions")
		}
		slog.Info("using DatabaseFactory to create database", "mode", "noop")
		db, err := options.DatabaseFactory(appCtx, "", nil, authz)
		if err != nil {
			return nil, fmt.Errorf("failed to create database via factory: %w", err)
		}
		return db, nil
	}

	baseDB, err := internaldb.NewPostgreSQL(dbCtx, cfg.DatabaseURL, authz, cfg.Embeddings.Enabled)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}
	if options.DatabaseFactory == nil {
		return baseDB, nil
	}
	wrapped, err := options.DatabaseFactory(appCtx, cfg.DatabaseURL, baseDB, authz)
	if err != nil {
		if closeErr := baseDB.Close(); closeErr != nil {
			slog.Error("error closing base database connection", "error", closeErr)
		}
		return nil, fmt.Errorf("failed to create extended database: %w", err)
	}
	return wrapped, nil
}

// startMCPServer wires the MCP HTTP bridge on cfg.MCPPort and launches it
// in a background goroutine. Returns nil when MCP is disabled (no port
// configured, or v1alpha1 Stores not wired — MCP is a consumer of the
// v1alpha1 data model and has nothing to serve without it). The returned
// *http.Server, when non-nil, should be shut down alongside the main
// server on quit.
func startMCPServer(
	cfg *config.Config,
	stores map[string]*v1alpha1store.Store,
	authnProvider auth.AuthnProvider,
) *http.Server {
	if cfg.MCPPort <= 0 {
		return nil
	}
	mcpServer := mcpregistry.NewServer(stores)
	var handler http.Handler = mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return mcpServer
	}, &mcp.StreamableHTTPOptions{})
	if authnProvider != nil {
		handler = mcpAuthnMiddleware(authnProvider)(handler)
	}
	addr := ":" + strconv.Itoa(int(cfg.MCPPort))
	srv := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		slog.Info("MCP HTTP server starting", "address", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("failed to start MCP server", "error", err)
			os.Exit(1)
		}
	}()
	return srv
}

// mcpAuthnMiddleware uses the AuthnProvider to attach a session to the
// request context on successful authentication. On auth error or missing
// session, the request continues with an unauthenticated context — the
// AuthzProvider downstream decides whether the request is allowed (the
// OSS default `PublicAuthzProvider` permits read-only access; enterprise
// authz can reject). Failing-open here is intentional so the MCP bridge
// works for anonymous `list_servers` / `get_server` traffic while still
// letting authenticated callers pick up privileged operations.
func mcpAuthnMiddleware(authn auth.AuthnProvider) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
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
// via the v1alpha1 Importer. Caller guarantees importer != nil.
func runSeedFromImport(cfg *config.Config, importer *pkgimporter.Importer) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ctx = auth.WithSystemContext(ctx)

	results, err := importer.Import(ctx, pkgimporter.Options{
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
func wireEmbeddings(cfg *config.Config, stores map[string]*v1alpha1store.Store, routeOpts *router.RouteOptions) {
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

	routeOpts.Indexer = idx
	routeOpts.SemanticSearch = makeSemanticSearchFunc(provider, cfg.Embeddings.Dimensions)
	slog.Info("embeddings indexer + semantic search enabled",
		"provider", cfg.Embeddings.Provider,
		"model", cfg.Embeddings.Model)
}

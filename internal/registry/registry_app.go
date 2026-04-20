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

	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpregistry "github.com/agentregistry-dev/agentregistry/internal/mcp/registryserver"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/jobs"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/kubernetes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/platforms/local"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1/registries"
	pkgimporter "github.com/agentregistry-dev/agentregistry/pkg/importer"
	osvscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/osv"
	scorecardscanner "github.com/agentregistry-dev/agentregistry/pkg/importer/scanners/scorecard"
	"github.com/agentregistry-dev/agentregistry/pkg/logging"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/jackc/pgx/v5/pgxpool"
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
		baseDB, err := internaldb.NewPostgreSQL(dbCtx, cfg.DatabaseURL, authz, cfg.DatabaseVectorEnabled)
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

	var embeddingProvider embeddings.Provider
	if cfg.Embeddings.Enabled {
		client := &http.Client{Timeout: 30 * time.Second}
		if provider, err := embeddings.Factory(&cfg.Embeddings, client); err != nil {
			slog.Warn("semantic embeddings disabled", "error", err)
		} else {
			embeddingProvider = provider
		}
	}

	serverService := serversvc.New(serversvc.Dependencies{
		Servers:            db.Servers(),
		Tx:                 db,
		Config:             cfg,
		EmbeddingsProvider: embeddingProvider,
	})
	agentService := agentsvc.New(agentsvc.Dependencies{
		Agents:             db.Agents(),
		Skills:             db.Skills(),
		Prompts:            db.Prompts(),
		Tx:                 db,
		Config:             cfg,
		EmbeddingsProvider: embeddingProvider,
	})
	providerService := providersvc.New(providersvc.Dependencies{
		StoreDB:           db,
		ProviderPlatforms: options.ProviderPlatforms,
	})
	providerPlatforms := providerService.PlatformAdapters()
	deploymentPlatforms := map[string]types.DeploymentPlatformAdapter{
		"local":      local.NewLocalDeploymentAdapter(serverService, agentService, cfg.RuntimeDir, cfg.AgentGatewayPort),
		"kubernetes": kubernetes.NewKubernetesDeploymentAdapter(providerService, serverService, agentService),
	}
	maps.Copy(deploymentPlatforms, options.DeploymentPlatforms)
	skillService := skillsvc.New(skillsvc.Dependencies{Skills: db.Skills(), Tx: db})
	promptService := promptsvc.New(promptsvc.Dependencies{Prompts: db.Prompts(), Tx: db})
	deploymentService := deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:            db,
		Deployments:        db.Deployments(),
		Providers:          providerService,
		Servers:            serverService,
		Agents:             agentService,
		DeploymentAdapters: deploymentPlatforms,
	})
	// Set up the v1alpha1 Stores + Importer bundle early so both the
	// seed goroutine below and the HTTP router later can use them.
	// Skipped for backends (noop / tests) that don't expose a
	// *pgxpool.Pool.
	var (
		v1alpha1Stores   map[string]*internaldb.Store
		v1alpha1Importer *pkgimporter.Importer
	)
	if pg, ok := db.(interface {
		Pool() *pgxpool.Pool
	}); ok {
		pool := pg.Pool()
		v1alpha1Stores = internaldb.NewV1Alpha1Stores(pool)

		// GITHUB_TOKEN (when set in env) authenticates scanner fetches
		// against GitHub's contents + repo API to raise the 60 req/hr
		// unauthenticated limit.
		githubToken := os.Getenv("GITHUB_TOKEN")
		imp, err := pkgimporter.New(pkgimporter.Config{
			Stores:   v1alpha1Stores,
			Findings: pkgimporter.NewFindingsStore(pool),
			Scanners: []pkgimporter.Scanner{
				osvscanner.New(osvscanner.Config{GitHubToken: githubToken}),
				scorecardscanner.New(scorecardscanner.Config{GitHubToken: githubToken}),
			},
			Resolver:          internaldb.NewV1Alpha1Resolver(v1alpha1Stores),
			RegistryValidator: registries.Dispatcher,
			UniqueRemoteURLs:  internaldb.NewV1Alpha1UniqueRemoteURLsChecker(v1alpha1Stores),
		})
		if err != nil {
			slog.Warn("failed to construct v1alpha1 importer; HTTP import + seed-from disabled for this path", "error", err)
		} else {
			v1alpha1Importer = imp
		}
		slog.Info("v1alpha1 routes enabled")
	} else {
		slog.Info("v1alpha1 routes disabled: database does not expose Pool() (likely noop/DatabaseFactory)")
	}

	// Import builtin seed data unless disabled. Writes to v1alpha1.*
	// tables via the generic Store. Skipped when the underlying DB
	// doesn't expose a pgxpool (noop/test backends) — seeding is
	// decorative for those anyway.
	if !cfg.DisableBuiltinSeed {
		if pg, ok := db.(interface {
			Pool() *pgxpool.Pool
		}); ok {
			slog.Info("importing builtin seed data in the background")
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
				defer cancel()
				ctx = auth.WithSystemContext(ctx)
				if err := seed.ImportBuiltinSeedDataV1Alpha1(ctx, pg.Pool()); err != nil {
					slog.Error("failed to import builtin seed data (v1alpha1)", "error", err)
				}
			}()
		} else {
			slog.Info("builtin seed skipped: database does not expose Pool()")
		}
	}

	// Import seed data if seed source is provided. Requires the
	// v1alpha1 Importer; backends without Pool() support can't seed
	// from disk in the new model.
	if cfg.SeedFrom != "" {
		if v1alpha1Importer == nil {
			slog.Warn("--seed-from requested but v1alpha1 importer unavailable; skipping", "seed_from", cfg.SeedFrom)
		} else {
			slog.Info("importing data in the background", "seed_from", cfg.SeedFrom)
			go runSeedFromImport(cfg, v1alpha1Importer)
		}
	}

	slog.Info("starting agentregistry", "version", version.Version, "commit", version.GitCommit)

	// Prepare version information
	versionInfo := &apitypes.VersionBody{
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

	routeOpts := &router.RouteOptions{
		ProviderPlatforms:   providerPlatforms,
		DeploymentPlatforms: deploymentPlatforms,
		ExtraRoutes:         options.ExtraRoutes,
	}

	// Reuse the v1alpha1 bundle constructed before the seed goroutines.
	// Nil stores means the underlying DB doesn't expose a pgxpool (noop
	// / DatabaseFactory backends); routes + import endpoint skip
	// registration in that case.
	routeOpts.V1Alpha1Stores = v1alpha1Stores
	routeOpts.V1Alpha1Importer = v1alpha1Importer

	// Initialize job manager and indexer for embeddings.
	if cfg.Embeddings.Enabled && embeddingProvider != nil {
		jobManager := jobs.NewManager()
		indexer := service.NewIndexer(serverService, agentService, embeddingProvider, cfg.Embeddings.Dimensions)
		routeOpts.Indexer = indexer
		routeOpts.JobManager = jobManager
		slog.Info("embeddings indexing API enabled")
	}

	// Initialize HTTP server
	baseServer := api.NewServer(cfg, router.RegistryServices{
		Server:     serverService,
		Agent:      agentService,
		Skill:      skillService,
		Prompt:     promptService,
		Provider:   providerService,
		Deployment: deploymentService,
	}, metrics, versionInfo, options.UIHandler, authnProvider, routeOpts)

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
	if cfg.MCPPort > 0 {
		mcpServer := mcpregistry.NewServer(serverService, agentService, skillService, deploymentService)

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

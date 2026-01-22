package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/importer"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func App(_ context.Context, opts ...types.AppOptions) error {
	var options types.AppOptions
	if len(opts) > 0 {
		options = opts[0]
	}
	cfg := config.NewConfig()
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// Create a context with timeout for PostgreSQL connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to PostgreSQL (runs OSS migrations)
	baseDB, err := internaldb.NewPostgreSQL(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Allow implementors to wrap the database, and run additional migrations
	var db database.Database = baseDB
	if options.DatabaseFactory != nil {
		db, err = options.DatabaseFactory(ctx, cfg.DatabaseURL, baseDB)
		if err != nil {
			if err := baseDB.Close(); err != nil {
				log.Printf("Error closing base database connection: %v", err)
			}
			return fmt.Errorf("failed to create extended database: %w", err)
		}
	}

	// Store the database instance for later cleanup
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing database connection: %v", err)
		} else {
			log.Println("Database connection closed successfully")
		}
	}()

	var embeddingProvider embeddings.Provider
	if cfg.Embeddings.Enabled {
		client := &http.Client{Timeout: 30 * time.Second}
		if provider, err := embeddings.Factory(&cfg.Embeddings, client); err != nil {
			log.Printf("Warning: semantic embeddings disabled: %v", err)
		} else {
			embeddingProvider = provider
		}
	}

	// Build authorizer from options
	jwtManager := auth.NewJWTManager(cfg)
	authzProvider := options.AuthzProvider
	if authzProvider == nil {
		authzProvider = auth.NewPublicAuthzProvider(jwtManager)
	}
	authz := auth.Authorizer{Authz: authzProvider}

	baseRegistryService := service.NewRegistryService(db, cfg, embeddingProvider, authz)

	var registryService service.RegistryService
	if options.ServiceFactory != nil {
		registryService = options.ServiceFactory(baseRegistryService)
	} else {
		registryService = baseRegistryService
	}

	if options.OnServiceCreated != nil {
		options.OnServiceCreated(registryService)
	}

	// Import builtin seed data unless it is disabled
	if !cfg.DisableBuiltinSeed {
		log.Printf("Importing builtin seed data in the background...")
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			if err := seed.ImportBuiltinSeedData(ctx, registryService); err != nil {
				log.Printf("Failed to import builtin seed data: %v", err)
			}
		}()
	}

	// Import seed data if seed source is provided
	if cfg.SeedFrom != "" {
		log.Printf("Importing data from %s in the background...", cfg.SeedFrom)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			importerService := importer.NewService(registryService)
			if embeddingProvider != nil {
				importerService.SetEmbeddingProvider(embeddingProvider)
				importerService.SetEmbeddingDimensions(cfg.Embeddings.Dimensions)
				importerService.SetGenerateEmbeddings(cfg.Embeddings.Enabled)
			}
			if err := importerService.ImportFromPath(ctx, cfg.SeedFrom, cfg.EnrichServerData); err != nil {
				log.Printf("Failed to import seed data: %v", err)
			}
		}()
	}

	log.Printf("Starting agentregistry %s (commit: %s)", version.Version, version.GitCommit)

	// Prepare version information
	versionInfo := &v0.VersionBody{
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
			log.Printf("Failed to shutdown telemetry: %v", err)
		}
	}()

	if cfg.ReconcileOnStartup {
		log.Println("Reconciling existing deployments at startup...")
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()

		if err := registryService.ReconcileAll(ctx); err != nil {
			log.Printf("Warning: Failed to reconcile deployments at startup: %v", err)
			log.Println("Server will continue starting, but deployments may not be in sync")
		} else {
			log.Println("Startup reconciliation completed successfully")
		}
	}

	// Initialize HTTP server
	baseServer := api.NewServer(cfg, registryService, metrics, versionInfo, options.UIHandler, options.AuthnProvider, options.AuthzProvider)

	var server types.Server
	if options.HTTPServerFactory != nil {
		server = options.HTTPServerFactory(baseServer)
	} else {
		server = baseServer
	}

	if options.OnHTTPServerCreated != nil {
		options.OnHTTPServerCreated(server)
	}

	// Start server in a goroutine so it doesn't block signal handling
	go func() {
		if err := server.Start(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("Failed to start server: %v", err)
			os.Exit(1)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)

	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	log.Println("Shutting down server...")

	// Create context with timeout for shutdown
	sctx, scancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer scancel()

	// Gracefully shutdown the server
	if err := server.Shutdown(sctx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("Server exiting")
	return nil
}

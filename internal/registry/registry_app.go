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

	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/version"

	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/importer"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
)

func App(_ context.Context) error {
	cfg := config.NewConfig()

	// Create a context with timeout for PostgreSQL connection
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Connect to PostgreSQL
	db, err := database.NewPostgreSQL(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("failed to connect to PostgreSQL: %w", err)
	}

	// Store the PostgreSQL instance for later cleanup
	defer func() {
		if err := db.Close(); err != nil {
			log.Printf("Error closing PostgreSQL connection: %v", err)
		} else {
			log.Println("PostgreSQL connection closed successfully")
		}
	}()

	registryService := service.NewRegistryService(db, cfg)

	// Import builtin seed data unless it is disabled
	if !cfg.DisableBuiltinSeed {
		log.Printf("Importing builtin seed data...")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		if err := seed.ImportBuiltinSeedData(ctx, registryService); err != nil {
			log.Printf("Failed to import builtin seed data: %v", err)
		}
	}

	// Import seed data if seed source is provided
	if cfg.SeedFrom != "" {
		log.Printf("Importing data from %s...", cfg.SeedFrom)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		importerService := importer.NewService(registryService)
		if err := importerService.ImportFromPath(ctx, cfg.SeedFrom, cfg.EnrichServerData); err != nil {
			log.Printf("Failed to import seed data: %v", err)
		}
	}

	log.Printf("Starting agentregistry v%s (commit: %s)", version.Version, version.GitCommit)

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
	server := api.NewServer(cfg, registryService, metrics, versionInfo)

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

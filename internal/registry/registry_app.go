package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	mcpauth "github.com/modelcontextprotocol/go-sdk/auth"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	mcpregistry "github.com/agentregistry-dev/agentregistry/internal/mcp/registryserver"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api"
	v0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	registryauth "github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	"github.com/agentregistry-dev/agentregistry/internal/registry/importer"
	"github.com/agentregistry-dev/agentregistry/internal/registry/seed"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/internal/version"

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

	var embeddingProvider embeddings.Provider
	if cfg.Embeddings.Enabled {
		client := &http.Client{Timeout: 30 * time.Second}
		if provider, err := embeddings.Factory(&cfg.Embeddings, client); err != nil {
			log.Printf("Warning: semantic embeddings disabled: %v", err)
		} else {
			embeddingProvider = provider
		}
	}

	baseRegistryService := service.NewRegistryService(db, cfg, embeddingProvider)

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
	baseServer := api.NewServer(cfg, registryService, metrics, versionInfo, options.UIHandler)

	var server types.Server
	if options.HTTPServerFactory != nil {
		server = options.HTTPServerFactory(baseServer)
	} else {
		server = baseServer
	}

	if options.OnHTTPServerCreated != nil {
		options.OnHTTPServerCreated(server)
	}

	var mcpHTTPServer *http.Server
	if cfg.MCPPort > 0 {
		var jwtManager *registryauth.JWTManager
		if cfg.JWTPrivateKey != "" {
			jwtManager = registryauth.NewJWTManager(cfg)
		}

		mcpServer := mcpregistry.NewServer(cfg, registryService)

		var handler http.Handler = mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return mcpServer
		}, &mcp.StreamableHTTPOptions{})

		if jwtManager != nil {
			verifier := func(ctx context.Context, token string, req *http.Request) (*mcpauth.TokenInfo, error) {
				claims, err := jwtManager.ValidateToken(ctx, token)
				if err != nil {
					return nil, fmt.Errorf("%w: %v", mcpauth.ErrInvalidToken, err)
				}
				exp := time.Unix(int64(claims.ExpiresAt.Unix()), 0)
				return &mcpauth.TokenInfo{
					Scopes:     []string{},
					Expiration: exp,
					UserID:     claims.AuthMethodSubject,
					Extra: map[string]any{
						"registry_claims": claims,
					},
				}, nil
			}
			handler = mcpauth.RequireBearerToken(verifier, nil)(handler)
		}

		addr := ":" + strconv.Itoa(int(cfg.MCPPort))
		mcpHTTPServer = &http.Server{
			Addr:              addr,
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
		}

		go func() {
			log.Printf("MCP HTTP server starting on %s", addr)
			if err := mcpHTTPServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				log.Printf("Failed to start MCP server: %v", err)
				os.Exit(1)
			}
		}()
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
	if mcpHTTPServer != nil {
		if err := mcpHTTPServer.Shutdown(sctx); err != nil {
			log.Printf("MCP server forced to shutdown: %v", err)
		}
	}

	log.Println("Server exiting")
	return nil
}

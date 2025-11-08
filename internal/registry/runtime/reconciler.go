package runtime

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service"
	"github.com/agentregistry-dev/agentregistry/internal/runtime"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/dockercompose"
	"github.com/agentregistry-dev/agentregistry/internal/runtime/translation/registry"
)

// Reconciler handles server-side reconciliation of deployed servers
type Reconciler interface {
	// ReconcileAll fetches all deployments from database and reconciles containers
	ReconcileAll(ctx context.Context) error
}

type reconciler struct {
	registryService  service.RegistryService
	runtimeDir       string
	agentGatewayPort uint16
	verbose          bool
}

// NewReconciler creates a new server-side reconciler
func NewReconciler(
	registryService service.RegistryService,
	runtimeDir string,
	agentGatewayPort uint16,
	verbose bool,
) Reconciler {
	return &reconciler{
		registryService:  registryService,
		runtimeDir:       runtimeDir,
		agentGatewayPort: agentGatewayPort,
		verbose:          verbose,
	}
}

// ReconcileAll fetches all deployments from database and reconciles containers
func (r *reconciler) ReconcileAll(ctx context.Context) error {
	// Get all deployed servers from database
	deployments, err := r.registryService.GetDeployments(ctx)
	if err != nil {
		return fmt.Errorf("failed to get deployed servers: %w", err)
	}

	log.Printf("Reconciling %d deployed server(s)", len(deployments))

	// If no servers remain, reconcile with empty list (will stop all containers)
	if len(deployments) == 0 {
		log.Println("No servers deployed, stopping all containers...")
		return r.reconcileServers(ctx, []*registry.MCPServerRunRequest{})
	}

	// Build run requests for ALL deployed servers
	var allRunRequests []*registry.MCPServerRunRequest
	for _, dep := range deployments {
		// Get server details from registry
		depServer, err := r.registryService.GetServerByNameAndVersion(ctx, dep.ServerName, dep.Version)
		if err != nil {
			log.Printf("Warning: Failed to get server %s v%s: %v", dep.ServerName, dep.Version, err)
			continue
		}

		// Parse config into env, arg, and header values
		depEnvValues := make(map[string]string)
		depArgValues := make(map[string]string)
		depHeaderValues := make(map[string]string)

		for k, v := range dep.Config {
			if len(k) > 7 && k[:7] == "HEADER_" {
				depHeaderValues[k[7:]] = v
			} else if len(k) > 4 && k[:4] == "ARG_" {
				depArgValues[k[4:]] = v
			} else {
				depEnvValues[k] = v
			}
		}

		allRunRequests = append(allRunRequests, &registry.MCPServerRunRequest{
			RegistryServer: &depServer.Server,
			PreferRemote:   dep.PreferRemote,
			EnvValues:      depEnvValues,
			ArgValues:      depArgValues,
			HeaderValues:   depHeaderValues,
		})
	}

	if len(allRunRequests) == 0 {
		return fmt.Errorf("no valid servers to reconcile")
	}

	log.Printf("Reconciling %d valid server(s)", len(allRunRequests))
	return r.reconcileServers(ctx, allRunRequests)
}

func (r *reconciler) reconcileServers(ctx context.Context, requests []*registry.MCPServerRunRequest) error {
	// Ensure runtime directory exists
	if err := os.MkdirAll(r.runtimeDir, 0755); err != nil {
		return fmt.Errorf("failed to create runtime directory: %w", err)
	}

	// Create runtime with translators
	regTranslator := registry.NewTranslator()
	composeTranslator := dockercompose.NewAgentGatewayTranslator(r.runtimeDir, r.agentGatewayPort)
	agentRuntime := runtime.NewAgentRegistryRuntime(
		regTranslator,
		composeTranslator,
		r.runtimeDir,
		r.verbose,
	)

	// Reconcile ALL servers
	if err := agentRuntime.ReconcileMCPServers(ctx, requests); err != nil {
		return fmt.Errorf("failed to reconcile servers: %w", err)
	}

	log.Println("Server reconciliation completed successfully")
	return nil
}

// GetDefaultRuntimeDir returns the default runtime directory for the server
func GetDefaultRuntimeDir() string {
	// Check for explicit override via environment variable
	if dir := os.Getenv("ARCTL_RUNTIME_DIR"); dir != "" {
		return dir
	}
	return "/tmp/arctl-runtime"
}

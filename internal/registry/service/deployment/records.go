package deployment

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/deployutil"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

// ResolveDeploymentAdapter returns the adapter registered for the given platform name.
func (s *registry) ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
	providerPlatform := strings.ToLower(strings.TrimSpace(platform))
	if providerPlatform == "" {
		return nil, fmt.Errorf("%w: deployment platform is required", database.ErrInvalidInput)
	}
	adapter, ok := s.adapters[providerPlatform]
	if !ok {
		return nil, &deployutil.UnsupportedDeploymentPlatformError{Platform: providerPlatform}
	}
	return adapter, nil
}

// ResolveDeploymentAdapterByProviderID looks up the provider by ID and returns its
// deployment adapter.
func (s *registry) ResolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error) {
	resolvedProviderID := strings.TrimSpace(providerID)
	if resolvedProviderID == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	provider, err := s.resolveProviderByID(ctx, resolvedProviderID)
	if err != nil {
		return nil, err
	}
	providerPlatform := strings.ToLower(strings.TrimSpace(provider.Platform))
	if providerPlatform == "" {
		return nil, fmt.Errorf("%w: provider platform is required", database.ErrInvalidInput)
	}
	return s.ResolveDeploymentAdapter(providerPlatform)
}

// CleanupExistingDeployment removes a stale managed deployment record for the given
// resource, undeploying from its platform if possible.
func (s *registry) CleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error {
	existing, err := s.findDeploymentByIdentity(ctx, resourceName, version, resourceType)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("looking up existing deployment: %w", err)
	}
	if existing == nil {
		return nil
	}

	cleanupPlatform, err := s.resolveExistingDeploymentCleanupPlatform(ctx, existing)
	if err != nil {
		return err
	}
	if cleanupPlatform == "" {
		log.Printf("Warning: skipping stale cleanup for deployment %s: provider platform unavailable", existing.ID)
	} else if err := s.cleanupStaleDeploymentOnPlatform(ctx, cleanupPlatform, existing); err != nil {
		log.Printf("Warning: failed stale cleanup for deployment %s on platform %s: %v", existing.ID, cleanupPlatform, err)
	}

	if err := s.deployments.DeleteDeployment(ctx, existing.ID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("removing stale deployment record: %w", err)
	}

	return nil
}

// CreateManagedDeploymentRecord validates the resource exists in the registry and
// writes a new deployment record in the Deploying state.
func (s *registry) CreateManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	now := time.Now()
	deployment := &models.Deployment{
		ID:               req.ID,
		ServerName:       strings.TrimSpace(req.ServerName),
		Version:          strings.TrimSpace(req.Version),
		Status:           models.DeploymentStatusDeploying,
		Env:              req.Env,
		ProviderConfig:   req.ProviderConfig,
		ProviderMetadata: req.ProviderMetadata,
		PreferRemote:     req.PreferRemote,
		ResourceType:     req.ResourceType,
		ProviderID:       req.ProviderID,
		Origin:           req.Origin,
		DeployedAt:       now,
		UpdatedAt:        now,
	}
	if deployment.ServerName == "" || deployment.Version == "" {
		return nil, fmt.Errorf("%w: resource name and version are required", database.ErrInvalidInput)
	}
	if deployment.Env == nil {
		deployment.Env = map[string]string{}
	}

	switch deployment.ResourceType {
	case resourceTypeMCP:
		serverResp, err := s.servers.GetServerVersion(ctx, deployment.ServerName, deployment.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, fmt.Errorf("server %s not found in registry: %w", deployment.ServerName, database.ErrNotFound)
			}
			return nil, fmt.Errorf("failed to verify server: %w", err)
		}
		deployment.Version = serverResp.Server.Version
	case resourceTypeAgent:
		agentResp, err := s.agents.GetAgentVersion(ctx, deployment.ServerName, deployment.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, fmt.Errorf("agent %s not found in registry: %w", deployment.ServerName, database.ErrNotFound)
			}
			return nil, fmt.Errorf("failed to verify agent: %w", err)
		}
		deployment.Version = agentResp.Agent.Version
	default:
		return nil, fmt.Errorf("%w: invalid resource type %q", database.ErrInvalidInput, deployment.ResourceType)
	}

	if err := s.deployments.CreateDeployment(ctx, deployment); err != nil {
		return nil, err
	}

	return s.deployments.GetDeployment(ctx, deployment.ID)
}

// ApplyDeploymentActionResult writes the result of a successful deployment action back
// to the deployment record.
func (s *registry) ApplyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
	status := models.DeploymentStatusDeployed
	if result != nil {
		if trimmedStatus := strings.TrimSpace(result.Status); trimmedStatus != "" {
			status = trimmedStatus
		}
	}

	errorText := ""
	patch := &models.DeploymentStatePatch{Status: &status, Error: &errorText}
	if result != nil {
		errorText = strings.TrimSpace(result.Error)
		patch.Error = &errorText
		if result.ProviderConfig != nil {
			cfg := result.ProviderConfig
			patch.ProviderConfig = &cfg
		}
		if result.ProviderMetadata != nil {
			meta := result.ProviderMetadata
			patch.ProviderMetadata = &meta
		}
	}

	return s.deployments.UpdateDeploymentState(auth.WithSystemContext(ctx), deploymentID, patch)
}

// ApplyFailedDeploymentAction writes failure state back to the deployment record after
// a deployment action returns an error.
func (s *registry) ApplyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error {
	status := models.DeploymentStatusFailed
	if result != nil {
		if trimmedStatus := strings.TrimSpace(result.Status); trimmedStatus != "" {
			status = trimmedStatus
		}
	}
	errorText := strings.TrimSpace(deployErr.Error())
	if result != nil && strings.TrimSpace(result.Error) != "" {
		errorText = strings.TrimSpace(result.Error)
	}

	patch := &models.DeploymentStatePatch{Status: &status, Error: &errorText}
	if result != nil {
		if result.ProviderConfig != nil {
			cfg := result.ProviderConfig
			patch.ProviderConfig = &cfg
		}
		if result.ProviderMetadata != nil {
			meta := result.ProviderMetadata
			patch.ProviderMetadata = &meta
		}
	}

	return s.deployments.UpdateDeploymentState(auth.WithSystemContext(ctx), deploymentID, patch)
}

func (s *registry) resolveProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	return s.providers.GetProvider(ctx, providerID)
}

func deploymentAdapterSupportsResourceType(adapter registrytypes.DeploymentPlatformAdapter, resourceType string) bool {
	if adapter == nil {
		return false
	}
	for _, supported := range adapter.SupportedResourceTypes() {
		if strings.EqualFold(strings.TrimSpace(supported), strings.TrimSpace(resourceType)) {
			return true
		}
	}
	return false
}

func (s *registry) findDeploymentByIdentity(ctx context.Context, resourceName, version, artifactType string) (*models.Deployment, error) {
	filter := &models.DeploymentFilter{ResourceType: &artifactType, ResourceName: &resourceName}
	deployments, err := s.deployments.ListDeployments(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, deployment := range deployments {
		if deployment.ServerName == resourceName && deployment.Version == version && deployment.ResourceType == artifactType {
			return deployment, nil
		}
	}
	return nil, database.ErrNotFound
}

func (s *registry) resolveExistingDeploymentCleanupPlatform(ctx context.Context, existing *models.Deployment) (string, error) {
	providerID := strings.TrimSpace(existing.ProviderID)
	if providerID == "" {
		return "", nil
	}

	provider, err := s.resolveProviderByID(ctx, providerID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("resolving provider for existing deployment: %w", err)
	}
	if provider == nil {
		return "", nil
	}
	return strings.ToLower(strings.TrimSpace(provider.Platform)), nil
}

func (s *registry) cleanupStaleDeploymentOnPlatform(ctx context.Context, cleanupPlatform string, existing *models.Deployment) error {
	adapter, err := s.ResolveDeploymentAdapter(cleanupPlatform)
	if err != nil {
		return fmt.Errorf("resolve deployment adapter: %w", err)
	}

	cleaner, ok := adapter.(deployutil.PlatformStaleCleaner)
	if !ok {
		return nil
	}
	return cleaner.CleanupStale(ctx, existing)
}

func (s *registry) removeDeploymentRecord(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	if deployment.ID == "" {
		return database.ErrInvalidInput
	}
	if deployment.Origin == originDiscovered {
		return database.ErrInvalidInput
	}

	return s.deployments.DeleteDeployment(ctx, deployment.ID)
}

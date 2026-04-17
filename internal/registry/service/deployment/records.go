package deployment

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// cleanupExistingDeployment removes a deployment both on its platform and in the DB.
// Caller supplies the existing record to avoid a second lookup.
//
// Behavior on platform resolution:
//   - Platform resolvable AND undeploy succeeds: DB record deleted. (happy path)
//   - Platform resolvable AND undeploy FAILS: error, DB record preserved so
//     the system state stays consistent with what actually happened on the platform.
//   - Platform NOT resolvable (provider deleted or transient): skip platform call
//     silently, still delete the DB record (the platform-side resource is already
//     orphaned and nothing we can do here; the DB record is stale).
func (s *registry) cleanupExistingDeployment(ctx context.Context, existing *models.Deployment) error {
	platform, err := s.resolveExistingDeploymentCleanupPlatform(ctx, existing)
	if err != nil {
		return err
	}
	if platform != "" {
		if err := s.cleanupStaleDeploymentOnPlatform(ctx, platform, existing); err != nil {
			return fmt.Errorf("platform undeploy failed for %s on %s: %w", existing.ID, platform, err)
		}
	}
	// Delete DB record (regardless of whether platform was resolvable).
	if err := s.deployments.DeleteDeployment(ctx, existing.ID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("removing stale deployment record: %w", err)
	}
	return nil
}

// CleanupExistingDeployment is the public entry point for callers without the
// existing record in hand. It looks up the record and delegates to cleanupExistingDeployment.
func (s *registry) CleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType, providerID string) error {
	existing, err := s.findDeploymentByIdentity(ctx, resourceName, version, resourceType, providerID)
	if err != nil {
		if errors.Is(err, database.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("looking up existing deployment: %w", err)
	}
	if existing == nil {
		return nil
	}
	return s.cleanupExistingDeployment(ctx, existing)
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

func (s *registry) findDeploymentByIdentity(ctx context.Context, resourceName, version, artifactType, providerID string) (*models.Deployment, error) {
	return findDeploymentByIdentityIn(ctx, s.deployments, resourceName, version, artifactType, providerID)
}

func findDeploymentByIdentityIn(ctx context.Context, store database.DeploymentStore, resourceName, version, artifactType, providerID string) (*models.Deployment, error) {
	filter := &models.DeploymentFilter{ResourceType: &artifactType, ResourceName: &resourceName}
	deployments, err := store.ListDeployments(ctx, filter)
	if err != nil {
		return nil, err
	}
	for _, deployment := range deployments {
		if deployment.ServerName == resourceName && deployment.Version == version && deployment.ResourceType == artifactType && deployment.ProviderID == providerID {
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
		if IsUnsupportedDeploymentPlatformError(err) {
			return nil
		}
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

// applyDeployment is the shared upsert logic for both agent and server deployments.
// It wraps the entire decision-and-launch in a transaction with an advisory lock
// so that concurrent applies for the same tuple serialize. It compares env,
// providerConfig, and preferRemote when determining drift.
func (s *registry) applyDeployment(ctx context.Context, resourceName, version, resourceType, providerID string,
	env map[string]string, providerConfig models.JSONObject, preferRemote, force bool,
) (*models.Deployment, error) {
	if resourceName == "" || version == "" || providerID == "" {
		return nil, fmt.Errorf("%w: resource name, version, and provider ID are required", database.ErrInvalidInput)
	}

	return database.InTransactionT(ctx, s.tx, func(txCtx context.Context, scope database.Scope) (*models.Deployment, error) {
		deployments := scope.Deployments()
		identity := fmt.Sprintf("%s/%s/%s/%s", resourceType, resourceName, version, providerID)
		if err := deployments.AcquireApplyLock(txCtx, identity); err != nil {
			return nil, err
		}

		existing, err := findDeploymentByIdentityIn(txCtx, deployments, resourceName, version, resourceType, providerID)
		if err != nil && !errors.Is(err, database.ErrNotFound) {
			return nil, fmt.Errorf("checking existing deployment: %w", err)
		}

		if existing == nil {
			return s.LaunchDeployment(txCtx, &models.Deployment{
				ServerName:     resourceName,
				Version:        version,
				ResourceType:   resourceType,
				ProviderID:     providerID,
				Env:            env,
				ProviderConfig: providerConfig,
				PreferRemote:   preferRemote,
			})
		}

		if existing.Status == models.DeploymentStatusDeployed &&
			envEqual(existing.Env, env) &&
			providerConfigEqual(existing.ProviderConfig, providerConfig) &&
			existing.PreferRemote == preferRemote {
			return existing, nil // identical desired state — idempotent no-op
		}

		// Drift is only an error for healthy (Deployed) deployments — those are "live"
		// and the user must explicitly acknowledge replacement via --force. For Failed,
		// Deploying, or otherwise non-healthy statuses we fall through to cleanup+launch
		// because the existing record doesn't represent a running deployment the user
		// would expect us to preserve.
		if existing.Status == models.DeploymentStatusDeployed && !force {
			return nil, fmt.Errorf("%w: deployment %s", ErrDeploymentDrift, existing.ID)
		}

		// cleanupExistingDeployment and LaunchDeployment below use the service's
		// default s.deployments store rather than scope.Deployments(), but they
		// still run inside this transaction: pgx propagates the active tx via
		// txCtx, so any s.deployments.Exec/Query call here is executed on the
		// same transaction that holds the advisory lock.
		if err := s.cleanupExistingDeployment(txCtx, existing); err != nil {
			return nil, fmt.Errorf("cleaning up stale deployment: %w", err)
		}

		return s.LaunchDeployment(txCtx, &models.Deployment{
			ServerName:     resourceName,
			Version:        version,
			ResourceType:   resourceType,
			ProviderID:     providerID,
			Env:            env,
			ProviderConfig: providerConfig,
			PreferRemote:   preferRemote,
		})
	})
}

func (s *registry) ApplyAgentDeployment(ctx context.Context, agentName, version, providerID string,
	env map[string]string, providerConfig models.JSONObject, preferRemote, force bool,
) (*models.Deployment, error) {
	return s.applyDeployment(ctx, agentName, version, resourceTypeAgent, providerID, env, providerConfig, preferRemote, force)
}

func (s *registry) ApplyServerDeployment(ctx context.Context, serverName, version, providerID string,
	env map[string]string, providerConfig models.JSONObject, preferRemote, force bool,
) (*models.Deployment, error) {
	return s.applyDeployment(ctx, serverName, version, resourceTypeMCP, providerID, env, providerConfig, preferRemote, force)
}

// envEqual returns true if a and b contain the same key/value pairs.
// nil and empty maps are treated as equal.
func envEqual(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		bv, ok := b[k]
		if !ok || bv != v {
			return false
		}
	}
	return true
}

// providerConfigEqual returns true if a and b serialize to equal canonical JSON.
// nil and empty objects are treated as equal. Map key ordering is irrelevant
// because encoding/json sorts keys.
func providerConfigEqual(a, b models.JSONObject) bool {
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	aBytes, errA := json.Marshal(a)
	bBytes, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(aBytes, bBytes)
}

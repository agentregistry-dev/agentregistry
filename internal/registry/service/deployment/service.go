package deployment

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

const (
	resourceTypeMCP   = "mcp"
	resourceTypeAgent = "agent"
	originDiscovered  = "discovered"
)

type Dependencies struct {
	StoreDB            database.ServiceDatabase
	Providers          database.ProviderStore
	Servers            database.ServerStore
	Agents             database.AgentStore
	Deployments        database.DeploymentStore
	DeploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
}

type Service struct {
	providers   database.ProviderStore
	servers     database.ServerStore
	agents      database.AgentStore
	deployments database.DeploymentStore
	adapters    map[string]registrytypes.DeploymentPlatformAdapter
}

func New(deps Dependencies) *Service {
	providers := deps.Providers
	if providers == nil {
		providers = deps.StoreDB
	}
	servers := deps.Servers
	if servers == nil {
		servers = deps.StoreDB
	}
	agents := deps.Agents
	if agents == nil {
		agents = deps.StoreDB
	}
	deployments := deps.Deployments
	if deployments == nil {
		deployments = deps.StoreDB
	}
	adapters := deps.DeploymentAdapters
	if adapters == nil {
		adapters = map[string]registrytypes.DeploymentPlatformAdapter{}
	}

	return &Service{
		providers:   providers,
		servers:     servers,
		agents:      agents,
		deployments: deployments,
		adapters:    adapters,
	}
}

func (s *Service) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	dbDeployments, err := s.deployments.GetDeployments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployments from DB: %w", err)
	}

	deployments := append([]*models.Deployment(nil), dbDeployments...)
	if shouldIncludeDiscoveredDeployments(filter) {
		deployments = s.appendDiscoveredDeployments(ctx, deployments, filter)
	}
	return deployments, nil
}

func (s *Service) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	deployment, err := s.deployments.GetDeploymentByID(ctx, id)
	if err == nil {
		return deployment, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	return s.getDiscoveredDeploymentByID(ctx, id)
}

func (s *Service) DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.CreateDeployment(ctx, &models.Deployment{
		ServerName:   serverName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeMCP,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

func (s *Service) DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.CreateDeployment(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeAgent,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

func (s *Service) RemoveDeploymentByID(ctx context.Context, id string) error {
	deployment, err := s.deployments.GetDeploymentByID(ctx, id)
	if err != nil {
		return err
	}
	return s.removeDeploymentRecord(ctx, deployment)
}

func (s *Service) CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: deployment request is required", database.ErrInvalidInput)
	}
	resourceType := strings.ToLower(strings.TrimSpace(req.ResourceType))
	if resourceType == "" {
		resourceType = resourceTypeMCP
	}
	if resourceType != resourceTypeMCP && resourceType != resourceTypeAgent {
		return nil, fmt.Errorf("%w: invalid resource type %q", database.ErrInvalidInput, req.ResourceType)
	}
	providerID := strings.TrimSpace(req.ProviderID)
	if providerID == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}

	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, providerID)
	if err != nil {
		return nil, err
	}
	if !deploymentAdapterSupportsResourceType(adapter, resourceType) {
		return nil, fmt.Errorf("%w: provider does not support resource type %q", database.ErrInvalidInput, resourceType)
	}

	deploymentReq := *req
	deploymentReq.ResourceType = resourceType
	deploymentReq.ProviderID = providerID
	deploymentReq.Origin = strings.TrimSpace(deploymentReq.Origin)
	if deploymentReq.Origin == "" {
		deploymentReq.Origin = "managed"
	}
	if deploymentReq.Env == nil {
		deploymentReq.Env = map[string]string{}
	}

	created, err := s.CreateManagedDeploymentRecord(ctx, &deploymentReq)
	if err != nil {
		return nil, err
	}

	actionResult, deployErr := adapter.Deploy(ctx, created)
	if deployErr != nil {
		if stateErr := s.ApplyFailedDeploymentAction(ctx, created.ID, deployErr, actionResult); stateErr != nil {
			return nil, fmt.Errorf("deploy failed: %w (state patch failed: %v)", deployErr, stateErr)
		}
		return nil, deployErr
	}

	if err := s.ApplyDeploymentActionResult(ctx, created.ID, actionResult); err != nil {
		return nil, err
	}

	return s.deployments.GetDeploymentByID(ctx, created.ID)
}

func (s *Service) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	if err := adapter.Undeploy(ctx, deployment); err != nil {
		return err
	}
	return s.removeDeploymentRecord(ctx, deployment)
}

func (s *Service) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if deployment == nil {
		return nil, database.ErrNotFound
	}
	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return nil, err
	}
	return adapter.GetLogs(ctx, deployment)
}

func (s *Service) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	return adapter.Cancel(ctx, deployment)
}

func (s *Service) ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
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

func (s *Service) ResolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error) {
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

func (s *Service) CleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error {
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

	if err := s.deployments.RemoveDeploymentByID(ctx, existing.ID); err != nil && !errors.Is(err, database.ErrNotFound) {
		return fmt.Errorf("removing stale deployment record: %w", err)
	}

	return nil
}

func (s *Service) CreateManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
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
		return nil, fmt.Errorf("%w: serverName and version are required", database.ErrInvalidInput)
	}
	if deployment.Env == nil {
		deployment.Env = map[string]string{}
	}

	switch deployment.ResourceType {
	case resourceTypeMCP:
		serverResp, err := s.servers.GetServerByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
		if err != nil {
			if errors.Is(err, database.ErrNotFound) {
				return nil, fmt.Errorf("server %s not found in registry: %w", deployment.ServerName, database.ErrNotFound)
			}
			return nil, fmt.Errorf("failed to verify server: %w", err)
		}
		deployment.Version = serverResp.Server.Version
	case resourceTypeAgent:
		agentResp, err := s.agents.GetAgentByNameAndVersion(ctx, deployment.ServerName, deployment.Version)
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

	return s.deployments.GetDeploymentByID(ctx, deployment.ID)
}

func (s *Service) ApplyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
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

func (s *Service) ApplyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error {
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

func shouldIncludeDiscoveredDeployments(filter *models.DeploymentFilter) bool {
	if filter == nil || filter.Origin == nil {
		return true
	}
	return strings.EqualFold(strings.TrimSpace(*filter.Origin), originDiscovered)
}

func DiscoveredDeploymentID(providerID, resourceType, name, version string) string {
	return DiscoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, "")
}

func DiscoveredDeploymentIDWithNamespace(providerID, resourceType, name, version, namespace string) string {
	raw := strings.ToLower(strings.TrimSpace(providerID)) + "|" +
		strings.ToLower(strings.TrimSpace(resourceType)) + "|" +
		strings.TrimSpace(name) + "|" +
		strings.TrimSpace(version) + "|" +
		strings.TrimSpace(namespace)
	sum := sha256.Sum256([]byte(raw))
	return "discovered-" + hex.EncodeToString(sum[:8])
}

func discoveredDeploymentNamespace(dep *models.Deployment) string {
	if dep == nil {
		return ""
	}
	meta := models.KubernetesProviderMetadata{}
	if err := dep.ProviderMetadata.UnmarshalInto(&meta); err == nil {
		if namespace := strings.TrimSpace(meta.Namespace); namespace != "" {
			return namespace
		}
	}
	return ""
}

func matchesDiscoveredDeploymentFilter(filter *models.DeploymentFilter, dep *models.Deployment, provider *models.Provider) bool {
	if filter == nil {
		return true
	}
	if filter.ProviderID != nil && strings.TrimSpace(dep.ProviderID) != strings.TrimSpace(*filter.ProviderID) {
		return false
	}
	if filter.Platform != nil && provider != nil && !strings.EqualFold(strings.TrimSpace(provider.Platform), strings.TrimSpace(*filter.Platform)) {
		return false
	}
	if filter.ResourceType != nil && dep.ResourceType != *filter.ResourceType {
		return false
	}
	if filter.Status != nil && dep.Status != *filter.Status {
		return false
	}
	if filter.Origin != nil && !strings.EqualFold(strings.TrimSpace(dep.Origin), strings.TrimSpace(*filter.Origin)) {
		return false
	}
	if filter.ResourceName != nil && !strings.Contains(strings.ToLower(dep.ServerName), strings.ToLower(*filter.ResourceName)) {
		return false
	}
	return true
}

func (s *Service) appendDiscoveredDeployments(ctx context.Context, deployments []*models.Deployment, filter *models.DeploymentFilter) []*models.Deployment {
	var platformFilter *string
	if filter != nil {
		platformFilter = filter.Platform
	}

	seenDeploymentIDs := make(map[string]struct{}, len(deployments))
	for _, dep := range deployments {
		if dep == nil {
			continue
		}
		if id := strings.TrimSpace(dep.ID); id != "" {
			seenDeploymentIDs[id] = struct{}{}
		}
	}

	providers, err := s.providers.ListProviders(ctx, platformFilter)
	if err != nil {
		log.Printf("Warning: Failed to list providers for discovery: %v", err)
		return deployments
	}

	for _, provider := range providers {
		if provider == nil {
			continue
		}
		if filter != nil && filter.ProviderID != nil && strings.TrimSpace(*filter.ProviderID) != "" &&
			!strings.EqualFold(strings.TrimSpace(provider.ID), strings.TrimSpace(*filter.ProviderID)) {
			continue
		}

		adapter, err := s.ResolveDeploymentAdapter(provider.Platform)
		if err != nil {
			log.Printf("Warning: Failed to resolve deployment adapter for provider %s (%s): %v", provider.ID, provider.Platform, err)
			continue
		}
		discovered, err := adapter.Discover(ctx, provider.ID)
		if err != nil {
			log.Printf("Warning: Failed to discover deployments for provider %s: %v", provider.ID, err)
			continue
		}

		for _, dep := range discovered {
			if dep == nil {
				continue
			}
			if strings.TrimSpace(dep.ProviderID) == "" {
				dep.ProviderID = provider.ID
			}
			if strings.TrimSpace(dep.Origin) == "" {
				dep.Origin = originDiscovered
			}
			if strings.TrimSpace(dep.ID) == "" {
				dep.ID = DiscoveredDeploymentIDWithNamespace(
					dep.ProviderID,
					dep.ResourceType,
					dep.ServerName,
					dep.Version,
					discoveredDeploymentNamespace(dep),
				)
			}
			if _, seen := seenDeploymentIDs[dep.ID]; seen {
				continue
			}
			if !matchesDiscoveredDeploymentFilter(filter, dep, provider) {
				continue
			}
			seenDeploymentIDs[dep.ID] = struct{}{}
			deployments = append(deployments, dep)
		}
	}

	return deployments
}

func (s *Service) getDiscoveredDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	discoveredID := strings.TrimSpace(id)
	if !strings.HasPrefix(discoveredID, "discovered-") {
		return nil, database.ErrNotFound
	}

	origin := originDiscovered
	deployments, err := s.GetDeployments(ctx, &models.DeploymentFilter{Origin: &origin})
	if err != nil {
		return nil, err
	}
	for _, dep := range deployments {
		if dep != nil && dep.ID == discoveredID {
			return dep, nil
		}
	}
	return nil, database.ErrNotFound
}

func (s *Service) resolveProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	if strings.TrimSpace(providerID) == "" {
		return nil, fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	return s.providers.GetProviderByID(ctx, providerID)
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

func (s *Service) findDeploymentByIdentity(ctx context.Context, resourceName, version, artifactType string) (*models.Deployment, error) {
	filter := &models.DeploymentFilter{ResourceType: &artifactType, ResourceName: &resourceName}
	deployments, err := s.deployments.GetDeployments(ctx, filter)
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

func (s *Service) resolveExistingDeploymentCleanupPlatform(ctx context.Context, existing *models.Deployment) (string, error) {
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

func (s *Service) cleanupStaleDeploymentOnPlatform(ctx context.Context, cleanupPlatform string, existing *models.Deployment) error {
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

func (s *Service) removeDeploymentRecord(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	if deployment.ID == "" {
		return database.ErrInvalidInput
	}
	if deployment.Origin == originDiscovered {
		return database.ErrInvalidInput
	}

	return s.deployments.RemoveDeploymentByID(ctx, deployment.ID)
}

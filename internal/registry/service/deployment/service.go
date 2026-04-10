package deployment

import (
	"context"
	"errors"
	"fmt"
	"strings"

	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/deployutil"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	resourceTypeMCP   = "mcp"
	resourceTypeAgent = "agent"
	originDiscovered  = "discovered"
)

// UnsupportedDeploymentPlatformError reports that no deployment adapter exists for a provider platform.
type UnsupportedDeploymentPlatformError = deployutil.UnsupportedDeploymentPlatformError

// IsUnsupportedDeploymentPlatformError reports whether err wraps an unsupported deployment platform error.
func IsUnsupportedDeploymentPlatformError(err error) bool {
	return deployutil.IsUnsupportedDeploymentPlatformError(err)
}

type Dependencies struct {
	StoreDB            database.Store
	Deployments        database.DeploymentStore
	Providers          providersvc.Registry
	ProviderPlatforms  map[string]registrytypes.ProviderPlatformAdapter
	Servers            serversvc.Registry
	Agents             agentsvc.Registry
	DeploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
}

type Registry interface {
	ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeployment(ctx context.Context, id string) (*models.Deployment, error)
	DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	DeleteDeployment(ctx context.Context, id string) error
	LaunchDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	UndeployDeployment(ctx context.Context, deployment *models.Deployment) error
	GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error)
	CancelDeployment(ctx context.Context, deployment *models.Deployment) error
}

type registry struct {
	deployments database.DeploymentStore
	providers   providersvc.Registry
	servers     serversvc.Registry
	agents      agentsvc.Registry
	adapters    map[string]registrytypes.DeploymentPlatformAdapter
}

var _ Registry = (*registry)(nil)

func New(deps Dependencies) Registry {
	if deps.Deployments == nil && deps.StoreDB != nil {
		deps.Deployments = deps.StoreDB.Deployments()
	}
	if deps.Providers == nil && deps.StoreDB != nil {
		deps.Providers = providersvc.New(providersvc.Dependencies{
			StoreDB:           deps.StoreDB,
			ProviderPlatforms: deps.ProviderPlatforms,
		})
	}
	if deps.Servers == nil && deps.StoreDB != nil {
		deps.Servers = serversvc.New(serversvc.Dependencies{StoreDB: deps.StoreDB})
	}
	if deps.Agents == nil && deps.StoreDB != nil {
		deps.Agents = agentsvc.New(agentsvc.Dependencies{StoreDB: deps.StoreDB})
	}

	adapters := deps.DeploymentAdapters
	if adapters == nil {
		adapters = map[string]registrytypes.DeploymentPlatformAdapter{}
	}

	return &registry{
		deployments: deps.Deployments,
		providers:   deps.Providers,
		servers:     deps.Servers,
		agents:      deps.Agents,
		adapters:    adapters,
	}
}

func (s *registry) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	dbDeployments, err := s.deployments.ListDeployments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployments from DB: %w", err)
	}

	deployments := append([]*models.Deployment(nil), dbDeployments...)
	if shouldIncludeDiscoveredDeployments(filter) {
		deployments = s.appendDiscoveredDeployments(ctx, deployments, filter)
	}
	return deployments, nil
}

func (s *registry) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	deployment, err := s.deployments.GetDeployment(ctx, id)
	if err == nil {
		return deployment, nil
	}
	if !errors.Is(err, database.ErrNotFound) {
		return nil, err
	}
	return s.getDiscoveredDeploymentByID(ctx, id)
}

func (s *registry) DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.LaunchDeployment(ctx, &models.Deployment{
		ServerName:   serverName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeMCP,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

func (s *registry) DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.LaunchDeployment(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: resourceTypeAgent,
		ProviderID:   providerID,
		Origin:       "managed",
	})
}

func (s *registry) DeleteDeployment(ctx context.Context, id string) error {
	deployment, err := s.deployments.GetDeployment(ctx, id)
	if err != nil {
		return err
	}
	return s.removeDeploymentRecord(ctx, deployment)
}

func (s *registry) LaunchDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
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

	return s.deployments.GetDeployment(ctx, created.ID)
}

func (s *registry) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
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

func (s *registry) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if deployment == nil {
		return nil, database.ErrNotFound
	}
	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return nil, err
	}
	return adapter.GetLogs(ctx, deployment)
}

func (s *registry) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	if deployment == nil {
		return database.ErrNotFound
	}
	adapter, err := s.ResolveDeploymentAdapterByProviderID(ctx, deployment.ProviderID)
	if err != nil {
		return err
	}
	return adapter.Cancel(ctx, deployment)
}

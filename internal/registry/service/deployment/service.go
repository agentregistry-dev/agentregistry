package deployment

import (
	"context"
	"fmt"

	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// Registry is the thin DB-only deployment surface that survived the
// v1alpha1 port. The adapter-dispatching verbs (DeployAgent / DeployServer /
// LaunchDeployment / ApplyAgentDeployment / ApplyServerDeployment /
// UndeployDeployment / GetDeploymentLogs / CancelDeployment) are gone —
// their v1alpha1 replacements live on V1Alpha1Coordinator + the
// /v0/namespaces/... endpoints. Remaining methods are read-only queries
// against the legacy public.deployments table, kept alive so the MCP
// bridge's list_deployments / get_deployment tools keep working until
// Group 9 ports them.
type Registry interface {
	ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeployment(ctx context.Context, id string) (*models.Deployment, error)
	DeleteDeployment(ctx context.Context, id string) error
}

// Dependencies bundles the inputs Registry needs. StoreDB is used when
// Deployments isn't explicitly supplied; Providers is retained so
// discovery-style queries can still join against providers, but no
// platform adapters are reachable from this surface.
type Dependencies struct {
	StoreDB     database.Store
	Deployments database.DeploymentStore
	Providers   providersvc.Registry
}

type registry struct {
	deployments database.DeploymentStore
	providers   providersvc.Registry
}

var _ Registry = (*registry)(nil)

// New constructs a thin Registry backed by the legacy deployment store.
// Provider dependency is optional — leave nil if the registry will only
// ever service by-ID queries.
func New(deps Dependencies) Registry {
	if deps.Deployments == nil && deps.StoreDB != nil {
		deps.Deployments = deps.StoreDB.Deployments()
	}
	if deps.Providers == nil && deps.StoreDB != nil {
		deps.Providers = providersvc.New(providersvc.Dependencies{StoreDB: deps.StoreDB})
	}
	return &registry{
		deployments: deps.Deployments,
		providers:   deps.Providers,
	}
}

func (s *registry) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	deployments, err := s.deployments.ListDeployments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to get deployments from DB: %w", err)
	}
	return deployments, nil
}

func (s *registry) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	return s.deployments.GetDeployment(ctx, id)
}

func (s *registry) DeleteDeployment(ctx context.Context, id string) error {
	if _, err := s.deployments.GetDeployment(ctx, id); err != nil {
		return err
	}
	return s.deployments.DeleteDeployment(ctx, id)
}

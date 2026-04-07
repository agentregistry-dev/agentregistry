package service

import (
	"context"
	"errors"
	"log/slog"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/embeddings"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type storeBundle struct {
	servers     database.ServerStore
	agents      database.AgentStore
	skills      database.SkillStore
	prompts     database.PromptStore
	providers   database.ProviderStore
	deployments database.DeploymentStore
}

func bundleFromScope(scope database.Scope) storeBundle {
	return storeBundle{
		servers:     safeScopeStore(scope.Servers),
		agents:      safeScopeStore(scope.Agents),
		skills:      safeScopeStore(scope.Skills),
		prompts:     safeScopeStore(scope.Prompts),
		providers:   safeScopeStore(scope.Providers),
		deployments: safeScopeStore(scope.Deployments),
	}
}

func safeScopeStore[T any](read func() T) (value T) {
	defer func() {
		if recover() != nil {
			var zero T
			value = zero
		}
	}()
	return read()
}

type registryServiceImpl struct {
	storeDB            database.Store
	serverRepo         database.ServerStore
	agentRepo          database.AgentStore
	skillRepo          database.SkillStore
	promptRepo         database.PromptStore
	providerRepo       database.ProviderStore
	deploymentRepo     database.DeploymentStore
	cfg                *config.Config
	embeddingsProvider embeddings.Provider
	deploymentAdapters map[string]registrytypes.DeploymentPlatformAdapter
	logger             *slog.Logger
}

func (s *registryServiceImpl) serviceDatabase() database.Store {
	return s.storeDB
}

func (s *registryServiceImpl) readStores() storeBundle {
	var stores storeBundle
	if storeDB := s.serviceDatabase(); storeDB != nil {
		stores = bundleFromScope(storeDB)
	}
	if s.serverRepo != nil {
		stores.servers = s.serverRepo
	}
	if s.agentRepo != nil {
		stores.agents = s.agentRepo
	}
	if s.skillRepo != nil {
		stores.skills = s.skillRepo
	}
	if s.promptRepo != nil {
		stores.prompts = s.promptRepo
	}
	if s.providerRepo != nil {
		stores.providers = s.providerRepo
	}
	if s.deploymentRepo != nil {
		stores.deployments = s.deploymentRepo
	}
	return stores
}

func (s *registryServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	storeDB := s.serviceDatabase()
	if storeDB == nil {
		return errors.New("store is not configured")
	}

	return storeDB.InTransaction(ctx, func(txCtx context.Context, scope database.Scope) error {
		return fn(txCtx, bundleFromScope(scope))
	})
}

func (s *registryServiceImpl) serverService() serversvc.Registry {
	stores := s.readStores()
	return serversvc.New(serversvc.Dependencies{
		Servers:            stores.servers,
		Tx:                 s.storeDB,
		Config:             s.cfg,
		EmbeddingsProvider: s.embeddingsProvider,
		Logger:             s.logger,
	})
}

func (s *registryServiceImpl) agentService() agentsvc.Registry {
	stores := s.readStores()
	return agentsvc.New(agentsvc.Dependencies{
		Agents:             stores.agents,
		Skills:             stores.skills,
		Prompts:            stores.prompts,
		Tx:                 s.storeDB,
		Config:             s.cfg,
		EmbeddingsProvider: s.embeddingsProvider,
		Logger:             s.logger,
	})
}

func (s *registryServiceImpl) skillService() skillsvc.Registry {
	stores := s.readStores()
	return skillsvc.New(skillsvc.Dependencies{Skills: stores.skills, Tx: s.storeDB})
}

func (s *registryServiceImpl) promptService() promptsvc.Registry {
	stores := s.readStores()
	return promptsvc.New(promptsvc.Dependencies{Prompts: stores.prompts, Tx: s.storeDB})
}

func (s *registryServiceImpl) providerService() providersvc.Registry {
	stores := s.readStores()
	return providersvc.New(providersvc.Dependencies{Providers: stores.providers})
}

type deploymentInternals interface {
	deploymentsvc.Registry
	ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error)
	ResolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error)
	CleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error
	CreateManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	ApplyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error
	ApplyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error
}

type deploymentServiceImpl struct {
	deploymentInternals
}

func (s *deploymentServiceImpl) resolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error) {
	return s.ResolveDeploymentAdapterByProviderID(ctx, providerID)
}

func (s *registryServiceImpl) deploymentService() *deploymentServiceImpl {
	stores := s.readStores()
	deploymentSvc := deploymentsvc.New(deploymentsvc.Dependencies{
		Deployments:        stores.deployments,
		Providers:          providersvc.New(providersvc.Dependencies{Providers: stores.providers}),
		Servers:            s.serverService(),
		Agents:             s.agentService(),
		DeploymentAdapters: s.deploymentAdapters,
	})
	internals, ok := deploymentSvc.(deploymentInternals)
	if !ok {
		panic("deployment service does not implement deploymentInternals")
	}
	return &deploymentServiceImpl{deploymentInternals: internals}
}

func (s *registryServiceImpl) BrowseServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.serverService().BrowseServers(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) LookupServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.serverService().LookupServer(ctx, serverName)
}

func (s *registryServiceImpl) LookupServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.serverService().LookupServerVersion(ctx, serverName, version)
}

func (s *registryServiceImpl) ServerHistory(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.serverService().ServerHistory(ctx, serverName)
}

func (s *registryServiceImpl) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return s.serverService().PublishServer(ctx, req)
}

func (s *registryServiceImpl) ReviseServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return s.serverService().ReviseServer(ctx, serverName, version, req, newStatus)
}

func (s *registryServiceImpl) SaveServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	return s.serverService().SaveServerReadme(ctx, serverName, version, content, contentType)
}

func (s *registryServiceImpl) LatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.serverService().LatestServerReadme(ctx, serverName)
}

func (s *registryServiceImpl) ServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.serverService().ServerReadme(ctx, serverName, version)
}

func (s *registryServiceImpl) RemoveServer(ctx context.Context, serverName, version string) error {
	return s.serverService().RemoveServer(ctx, serverName, version)
}

func (s *registryServiceImpl) SaveServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.serverService().SaveServerEmbedding(ctx, serverName, version, embedding)
}

func (s *registryServiceImpl) ServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.serverService().ServerEmbeddingMetadata(ctx, serverName, version)
}

func (s *registryServiceImpl) BrowseAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.agentService().BrowseAgents(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) LookupAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.agentService().LookupAgent(ctx, agentName)
}

func (s *registryServiceImpl) LookupAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.agentService().LookupAgentVersion(ctx, agentName, version)
}

func (s *registryServiceImpl) AgentHistory(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.agentService().AgentHistory(ctx, agentName)
}

func (s *registryServiceImpl) PublishAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return s.agentService().PublishAgent(ctx, req)
}

func (s *registryServiceImpl) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return s.agentService().ResolveAgentManifestSkills(ctx, manifest)
}

func (s *registryServiceImpl) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return s.agentService().ResolveAgentManifestPrompts(ctx, manifest)
}

func (s *registryServiceImpl) RemoveAgent(ctx context.Context, agentName, version string) error {
	return s.agentService().RemoveAgent(ctx, agentName, version)
}

func (s *registryServiceImpl) SaveAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.agentService().SaveAgentEmbedding(ctx, agentName, version, embedding)
}

func (s *registryServiceImpl) AgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.agentService().AgentEmbeddingMetadata(ctx, agentName, version)
}

func (s *registryServiceImpl) BrowseSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return s.skillService().BrowseSkills(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) LookupSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.skillService().LookupSkill(ctx, skillName)
}

func (s *registryServiceImpl) LookupSkillVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.skillService().LookupSkillVersion(ctx, skillName, version)
}

func (s *registryServiceImpl) SkillHistory(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.skillService().SkillHistory(ctx, skillName)
}

func (s *registryServiceImpl) PublishSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return s.skillService().PublishSkill(ctx, req)
}

func (s *registryServiceImpl) RemoveSkill(ctx context.Context, skillName, version string) error {
	return s.skillService().RemoveSkill(ctx, skillName, version)
}

func (s *registryServiceImpl) BrowsePrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return s.promptService().BrowsePrompts(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) LookupPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.promptService().LookupPrompt(ctx, promptName)
}

func (s *registryServiceImpl) LookupPromptVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.promptService().LookupPromptVersion(ctx, promptName, version)
}

func (s *registryServiceImpl) PromptHistory(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.promptService().PromptHistory(ctx, promptName)
}

func (s *registryServiceImpl) PublishPrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return s.promptService().PublishPrompt(ctx, req)
}

func (s *registryServiceImpl) RemovePrompt(ctx context.Context, promptName, version string) error {
	return s.promptService().RemovePrompt(ctx, promptName, version)
}

func (s *registryServiceImpl) BrowseProviders(ctx context.Context, platform string) ([]*models.Provider, error) {
	return s.providerService().BrowseProviders(ctx, platform)
}

func (s *registryServiceImpl) LookupProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.providerService().LookupProvider(ctx, providerID)
}

func (s *registryServiceImpl) RegisterProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.providerService().RegisterProvider(ctx, in)
}

func (s *registryServiceImpl) ResolveProvider(ctx context.Context, providerID, platformHint string) (*models.Provider, error) {
	return s.providerService().ResolveProvider(ctx, providerID, platformHint)
}

func (s *registryServiceImpl) ReviseProvider(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.providerService().ReviseProvider(ctx, providerID, platformHint, in)
}

func (s *registryServiceImpl) RemoveProvider(ctx context.Context, providerID, platformHint string) error {
	return s.providerService().RemoveProvider(ctx, providerID, platformHint)
}

func (s *registryServiceImpl) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.readStores().providers.ListProviders(ctx, platform)
}

func (s *registryServiceImpl) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.readStores().providers.GetProviderByID(ctx, providerID)
}

func (s *registryServiceImpl) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.readStores().providers.CreateProvider(ctx, in)
}

func (s *registryServiceImpl) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.readStores().providers.UpdateProvider(ctx, providerID, in)
}

func (s *registryServiceImpl) DeleteProvider(ctx context.Context, providerID string) error {
	return s.readStores().providers.DeleteProvider(ctx, providerID)
}

func (s *registryServiceImpl) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return s.deploymentService().GetDeployments(ctx, filter)
}

func (s *registryServiceImpl) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	return s.deploymentService().GetDeploymentByID(ctx, id)
}

func (s *registryServiceImpl) DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.deploymentService().DeployServer(ctx, serverName, version, env, preferRemote, providerID)
}

func (s *registryServiceImpl) DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.deploymentService().DeployAgent(ctx, agentName, version, env, preferRemote, providerID)
}

func (s *registryServiceImpl) RemoveDeploymentByID(ctx context.Context, id string) error {
	return s.deploymentService().RemoveDeploymentByID(ctx, id)
}

func (s *registryServiceImpl) CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return s.deploymentService().CreateDeployment(ctx, req)
}

func (s *registryServiceImpl) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	return s.deploymentService().UndeployDeployment(ctx, deployment)
}

func (s *registryServiceImpl) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	return s.deploymentService().GetDeploymentLogs(ctx, deployment)
}

func (s *registryServiceImpl) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	return s.deploymentService().CancelDeployment(ctx, deployment)
}

func (s *registryServiceImpl) resolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
	return s.deploymentService().ResolveDeploymentAdapter(platform)
}

func (s *registryServiceImpl) cleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error {
	return s.deploymentService().CleanupExistingDeployment(ctx, resourceName, version, resourceType)
}

func (s *registryServiceImpl) createManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return s.deploymentService().CreateManagedDeploymentRecord(ctx, req)
}

func (s *registryServiceImpl) applyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
	return s.deploymentService().ApplyDeploymentActionResult(ctx, deploymentID, result)
}

func (s *registryServiceImpl) applyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error {
	return s.deploymentService().ApplyFailedDeploymentAction(ctx, deploymentID, deployErr, result)
}

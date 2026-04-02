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

func bundleFromStore(store database.Store) storeBundle {
	return storeBundle{
		servers:     store,
		agents:      store,
		skills:      store,
		prompts:     store,
		providers:   store,
		deployments: store,
	}
}

type registryServiceImpl struct {
	storeDB            database.ServiceDatabase
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

func (s *registryServiceImpl) serviceDatabase() database.ServiceDatabase {
	return s.storeDB
}

func (s *registryServiceImpl) readStores() storeBundle {
	var stores storeBundle
	if storeDB := s.serviceDatabase(); storeDB != nil {
		stores = bundleFromStore(storeDB)
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
		return errors.New("service database is not configured")
	}

	return storeDB.InTransaction(ctx, func(txCtx context.Context, store database.Store) error {
		return fn(txCtx, bundleFromStore(store))
	})
}

func (s *registryServiceImpl) serverService() *serversvc.Service {
	stores := s.readStores()
	return serversvc.New(serversvc.Dependencies{
		StoreDB:            s.storeDB,
		Servers:            stores.servers,
		Config:             s.cfg,
		EmbeddingsProvider: s.embeddingsProvider,
		Logger:             s.logger,
	})
}

func (s *registryServiceImpl) agentService() *agentsvc.Service {
	stores := s.readStores()
	return agentsvc.New(agentsvc.Dependencies{
		StoreDB:            s.storeDB,
		Agents:             stores.agents,
		Skills:             stores.skills,
		Prompts:            stores.prompts,
		Config:             s.cfg,
		EmbeddingsProvider: s.embeddingsProvider,
		Logger:             s.logger,
	})
}

func (s *registryServiceImpl) skillService() *skillsvc.Service {
	stores := s.readStores()
	return skillsvc.New(skillsvc.Dependencies{StoreDB: s.storeDB, Skills: stores.skills})
}

func (s *registryServiceImpl) promptService() *promptsvc.Service {
	stores := s.readStores()
	return promptsvc.New(promptsvc.Dependencies{StoreDB: s.storeDB, Prompts: stores.prompts})
}

func (s *registryServiceImpl) providerService() *providersvc.Service {
	stores := s.readStores()
	return providersvc.New(providersvc.Dependencies{StoreDB: s.storeDB, Providers: stores.providers})
}

type deploymentServiceImpl struct {
	*deploymentsvc.Service
}

func (s *deploymentServiceImpl) resolveDeploymentAdapterByProviderID(ctx context.Context, providerID string) (registrytypes.DeploymentPlatformAdapter, error) {
	return s.ResolveDeploymentAdapterByProviderID(ctx, providerID)
}

func (s *registryServiceImpl) deploymentService() *deploymentServiceImpl {
	stores := s.readStores()
	return &deploymentServiceImpl{Service: deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:            s.storeDB,
		Providers:          stores.providers,
		Servers:            stores.servers,
		Agents:             stores.agents,
		Deployments:        stores.deployments,
		DeploymentAdapters: s.deploymentAdapters,
	})}
}

func (s *registryServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.serverService().ListServers(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.serverService().GetServerByName(ctx, serverName)
}

func (s *registryServiceImpl) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.serverService().GetServerByNameAndVersion(ctx, serverName, version)
}

func (s *registryServiceImpl) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.serverService().GetAllVersionsByServerName(ctx, serverName)
}

func (s *registryServiceImpl) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return s.serverService().CreateServer(ctx, req)
}

func (s *registryServiceImpl) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return s.serverService().UpdateServer(ctx, serverName, version, req, newStatus)
}

func (s *registryServiceImpl) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	return s.serverService().StoreServerReadme(ctx, serverName, version, content, contentType)
}

func (s *registryServiceImpl) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.serverService().GetServerReadmeLatest(ctx, serverName)
}

func (s *registryServiceImpl) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.serverService().GetServerReadmeByVersion(ctx, serverName, version)
}

func (s *registryServiceImpl) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.serverService().DeleteServer(ctx, serverName, version)
}

func (s *registryServiceImpl) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.serverService().UpsertServerEmbedding(ctx, serverName, version, embedding)
}

func (s *registryServiceImpl) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.serverService().GetServerEmbeddingMetadata(ctx, serverName, version)
}

func (s *registryServiceImpl) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.agentService().ListAgents(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.agentService().GetAgentByName(ctx, agentName)
}

func (s *registryServiceImpl) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.agentService().GetAgentByNameAndVersion(ctx, agentName, version)
}

func (s *registryServiceImpl) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.agentService().GetAllVersionsByAgentName(ctx, agentName)
}

func (s *registryServiceImpl) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return s.agentService().CreateAgent(ctx, req)
}

func (s *registryServiceImpl) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return s.agentService().ResolveAgentManifestSkills(ctx, manifest)
}

func (s *registryServiceImpl) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return s.agentService().ResolveAgentManifestPrompts(ctx, manifest)
}

func (s *registryServiceImpl) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.agentService().DeleteAgent(ctx, agentName, version)
}

func (s *registryServiceImpl) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.agentService().UpsertAgentEmbedding(ctx, agentName, version, embedding)
}

func (s *registryServiceImpl) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.agentService().GetAgentEmbeddingMetadata(ctx, agentName, version)
}

func (s *registryServiceImpl) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return s.skillService().ListSkills(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.skillService().GetSkillByName(ctx, skillName)
}

func (s *registryServiceImpl) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.skillService().GetSkillByNameAndVersion(ctx, skillName, version)
}

func (s *registryServiceImpl) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.skillService().GetAllVersionsBySkillName(ctx, skillName)
}

func (s *registryServiceImpl) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return s.skillService().CreateSkill(ctx, req)
}

func (s *registryServiceImpl) DeleteSkill(ctx context.Context, skillName, version string) error {
	return s.skillService().DeleteSkill(ctx, skillName, version)
}

func (s *registryServiceImpl) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return s.promptService().ListPrompts(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.promptService().GetPromptByName(ctx, promptName)
}

func (s *registryServiceImpl) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.promptService().GetPromptByNameAndVersion(ctx, promptName, version)
}

func (s *registryServiceImpl) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.promptService().GetAllVersionsByPromptName(ctx, promptName)
}

func (s *registryServiceImpl) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return s.promptService().CreatePrompt(ctx, req)
}

func (s *registryServiceImpl) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.promptService().DeletePrompt(ctx, promptName, version)
}

func (s *registryServiceImpl) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.providerService().ListProviders(ctx, platform)
}

func (s *registryServiceImpl) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.providerService().GetProviderByID(ctx, providerID)
}

func (s *registryServiceImpl) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.providerService().CreateProvider(ctx, in)
}

func (s *registryServiceImpl) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.providerService().UpdateProvider(ctx, providerID, in)
}

func (s *registryServiceImpl) DeleteProvider(ctx context.Context, providerID string) error {
	return s.providerService().DeleteProvider(ctx, providerID)
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

func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
	if servers == nil {
		servers = s.readStores().servers
	}
	return s.serverService().ValidateNoDuplicateRemoteURLs(ctx, servers, serverDetail)
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

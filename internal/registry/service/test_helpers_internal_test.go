package service

import (
	"context"
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

// registryServiceImpl is a test-only aggregate facade that wires all domain service
// packages behind a single struct. It exists so that cross-domain integration tests
// can share a common backing store while calling each domain service through its own
// Registry interface. It is not a production seam and should not be treated as one.
//
// Individual domain services are constructed on demand from the current store bundle
// (see serverService, agentService, etc.) to support partial store injection in tests.
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

func (s *registryServiceImpl) deploymentService() deploymentsvc.Registry {
	stores := s.readStores()
	return deploymentsvc.New(deploymentsvc.Dependencies{
		Deployments:        stores.deployments,
		Providers:          providersvc.New(providersvc.Dependencies{Providers: stores.providers}),
		Servers:            s.serverService(),
		Agents:             s.agentService(),
		DeploymentAdapters: s.deploymentAdapters,
	})
}

func (s *registryServiceImpl) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.serverService().ListServers(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.serverService().GetServer(ctx, serverName)
}

func (s *registryServiceImpl) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.serverService().GetServerVersion(ctx, serverName, version)
}

func (s *registryServiceImpl) GetServerVersions(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.serverService().GetServerVersions(ctx, serverName)
}

func (s *registryServiceImpl) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return s.serverService().PublishServer(ctx, req)
}

func (s *registryServiceImpl) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return s.serverService().UpdateServer(ctx, serverName, version, req, newStatus)
}

func (s *registryServiceImpl) SetServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	return s.serverService().SetServerReadme(ctx, serverName, version, content, contentType)
}

func (s *registryServiceImpl) GetLatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.serverService().GetLatestServerReadme(ctx, serverName)
}

func (s *registryServiceImpl) GetServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.serverService().GetServerReadme(ctx, serverName, version)
}

func (s *registryServiceImpl) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.serverService().DeleteServer(ctx, serverName, version)
}

func (s *registryServiceImpl) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return s.serverService().SetServerEmbedding(ctx, serverName, version, embedding)
}

func (s *registryServiceImpl) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.serverService().GetServerEmbeddingMetadata(ctx, serverName, version)
}

func (s *registryServiceImpl) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.agentService().ListAgents(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.agentService().GetAgent(ctx, agentName)
}

func (s *registryServiceImpl) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.agentService().GetAgentVersion(ctx, agentName, version)
}

func (s *registryServiceImpl) GetAgentVersions(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.agentService().GetAgentVersions(ctx, agentName)
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

func (s *registryServiceImpl) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.agentService().DeleteAgent(ctx, agentName, version)
}

func (s *registryServiceImpl) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return s.agentService().SetAgentEmbedding(ctx, agentName, version, embedding)
}

func (s *registryServiceImpl) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return s.agentService().GetAgentEmbeddingMetadata(ctx, agentName, version)
}

func (s *registryServiceImpl) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return s.skillService().ListSkills(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.skillService().GetSkill(ctx, skillName)
}

func (s *registryServiceImpl) GetSkillVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.skillService().GetSkillVersion(ctx, skillName, version)
}

func (s *registryServiceImpl) GetSkillVersions(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.skillService().GetSkillVersions(ctx, skillName)
}

func (s *registryServiceImpl) PublishSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return s.skillService().PublishSkill(ctx, req)
}

func (s *registryServiceImpl) DeleteSkill(ctx context.Context, skillName, version string) error {
	return s.skillService().DeleteSkill(ctx, skillName, version)
}

func (s *registryServiceImpl) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return s.promptService().ListPrompts(ctx, filter, cursor, limit)
}

func (s *registryServiceImpl) GetPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.promptService().GetPrompt(ctx, promptName)
}

func (s *registryServiceImpl) GetPromptVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.promptService().GetPromptVersion(ctx, promptName, version)
}

func (s *registryServiceImpl) GetPromptVersions(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.promptService().GetPromptVersions(ctx, promptName)
}

func (s *registryServiceImpl) PublishPrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	return s.promptService().PublishPrompt(ctx, req)
}

func (s *registryServiceImpl) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.promptService().DeletePrompt(ctx, promptName, version)
}

func (s *registryServiceImpl) ListProviders(ctx context.Context, platform string) ([]*models.Provider, error) {
	return s.providerService().ListProviders(ctx, platform)
}

func (s *registryServiceImpl) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.providerService().GetProvider(ctx, providerID)
}

func (s *registryServiceImpl) RegisterProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.providerService().RegisterProvider(ctx, in)
}

func (s *registryServiceImpl) ResolveProvider(ctx context.Context, providerID, platformHint string) (*models.Provider, error) {
	return s.providerService().ResolveProvider(ctx, providerID, platformHint)
}

func (s *registryServiceImpl) UpdateProvider(ctx context.Context, providerID, platformHint string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.providerService().UpdateProvider(ctx, providerID, platformHint, in)
}

func (s *registryServiceImpl) DeleteProvider(ctx context.Context, providerID, platformHint string) error {
	return s.providerService().DeleteProvider(ctx, providerID, platformHint)
}

func (s *registryServiceImpl) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return s.deploymentService().ListDeployments(ctx, filter)
}

func (s *registryServiceImpl) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	return s.deploymentService().GetDeployment(ctx, id)
}

func (s *registryServiceImpl) DeployServer(ctx context.Context, serverName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.deploymentService().DeployServer(ctx, serverName, version, env, preferRemote, providerID)
}

func (s *registryServiceImpl) DeployAgent(ctx context.Context, agentName, version string, env map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return s.deploymentService().DeployAgent(ctx, agentName, version, env, preferRemote, providerID)
}

func (s *registryServiceImpl) DeleteDeployment(ctx context.Context, id string) error {
	return s.deploymentService().DeleteDeployment(ctx, id)
}

func (s *registryServiceImpl) LaunchDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return s.deploymentService().LaunchDeployment(ctx, req)
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

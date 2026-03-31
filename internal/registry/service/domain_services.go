package service

import (
	"context"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Domain views give each service interface its own concrete receiver while
// still sharing the same registry service state.
type serverServiceImpl registryServiceImpl

type agentServiceImpl registryServiceImpl

type skillServiceImpl registryServiceImpl

type promptServiceImpl registryServiceImpl

type providerServiceImpl registryServiceImpl

type deploymentServiceImpl registryServiceImpl

var (
	_ RegistryService   = (*registryServiceImpl)(nil)
	_ ServerService     = (*serverServiceImpl)(nil)
	_ AgentService      = (*agentServiceImpl)(nil)
	_ SkillService      = (*skillServiceImpl)(nil)
	_ PromptService     = (*promptServiceImpl)(nil)
	_ ProviderService   = (*providerServiceImpl)(nil)
	_ DeploymentService = (*deploymentServiceImpl)(nil)
)

func (s *registryServiceImpl) serverService() *serverServiceImpl {
	return (*serverServiceImpl)(s)
}

func (s *registryServiceImpl) agentService() *agentServiceImpl {
	return (*agentServiceImpl)(s)
}

func (s *registryServiceImpl) skillService() *skillServiceImpl {
	return (*skillServiceImpl)(s)
}

func (s *registryServiceImpl) promptService() *promptServiceImpl {
	return (*promptServiceImpl)(s)
}

func (s *registryServiceImpl) providerService() *providerServiceImpl {
	return (*providerServiceImpl)(s)
}

func (s *registryServiceImpl) deploymentService() *deploymentServiceImpl {
	return (*deploymentServiceImpl)(s)
}

func (s *serverServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *serverServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *serverServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	return s.root().inTransaction(ctx, fn)
}

func (s *serverServiceImpl) ensureSemanticEmbedding(ctx context.Context, opts *database.SemanticSearchOptions) error {
	return s.root().ensureSemanticEmbedding(ctx, opts)
}

func (s *serverServiceImpl) shouldGenerateEmbeddingsOnPublish() bool {
	return s.root().shouldGenerateEmbeddingsOnPublish()
}

func (s *agentServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *agentServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *agentServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	return s.root().inTransaction(ctx, fn)
}

func (s *agentServiceImpl) ensureSemanticEmbedding(ctx context.Context, opts *database.SemanticSearchOptions) error {
	return s.root().ensureSemanticEmbedding(ctx, opts)
}

func (s *agentServiceImpl) shouldGenerateEmbeddingsOnPublish() bool {
	return s.root().shouldGenerateEmbeddingsOnPublish()
}

func (s *agentServiceImpl) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.root().skillService().GetSkillByNameAndVersion(ctx, skillName, version)
}

func (s *agentServiceImpl) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.root().promptService().GetPromptByName(ctx, promptName)
}

func (s *agentServiceImpl) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.root().promptService().GetPromptByNameAndVersion(ctx, promptName, version)
}

func (s *skillServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *skillServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *skillServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	return s.root().inTransaction(ctx, fn)
}

func (s *promptServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *promptServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *promptServiceImpl) inTransaction(ctx context.Context, fn func(context.Context, storeBundle) error) error {
	return s.root().inTransaction(ctx, fn)
}

func (s *providerServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *providerServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *deploymentServiceImpl) root() *registryServiceImpl {
	return (*registryServiceImpl)(s)
}

func (s *deploymentServiceImpl) readStores() storeBundle {
	return s.root().readStores()
}

func (s *deploymentServiceImpl) resolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
	return s.root().resolveDeploymentAdapter(platform)
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

func (s *registryServiceImpl) validateNoDuplicateRemoteURLs(ctx context.Context, servers database.ServerStore, serverDetail apiv0.ServerJSON) error {
	return s.serverService().validateNoDuplicateRemoteURLs(ctx, servers, serverDetail)
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

func (s *registryServiceImpl) cleanupExistingDeployment(ctx context.Context, resourceName, version, resourceType string) error {
	return s.deploymentService().cleanupExistingDeployment(ctx, resourceName, version, resourceType)
}

func (s *registryServiceImpl) createManagedDeploymentRecord(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return s.deploymentService().createManagedDeploymentRecord(ctx, req)
}

func (s *registryServiceImpl) applyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
	return s.deploymentService().applyDeploymentActionResult(ctx, deploymentID, result)
}

func (s *registryServiceImpl) applyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error {
	return s.deploymentService().applyFailedDeploymentAction(ctx, deploymentID, deployErr, result)
}

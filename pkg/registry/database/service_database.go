package database

import (
	"context"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type ServerStore interface {
	DeleteServer(ctx context.Context, serverName, version string) error
	CreateServer(ctx context.Context, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error)
	UpdateServer(ctx context.Context, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	SetServerStatus(ctx context.Context, serverName, version, status string) (*apiv0.ServerResponse, error)
	ListServers(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	GetCurrentLatestVersion(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	CountServerVersions(ctx context.Context, serverName string) (int, error)
	CheckVersionExists(ctx context.Context, serverName, version string) (bool, error)
	UnmarkAsLatest(ctx context.Context, serverName string) error
	AcquireServerCreateLock(ctx context.Context, serverName string) error
	SetServerEmbedding(ctx context.Context, serverName, version string, embedding *SemanticEmbedding) error
	GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*SemanticEmbeddingMetadata, error)
	UpsertServerReadme(ctx context.Context, readme *ServerReadme) error
	GetServerReadme(ctx context.Context, serverName, version string) (*ServerReadme, error)
	GetLatestServerReadme(ctx context.Context, serverName string) (*ServerReadme, error)
}

type ProviderStore interface {
	CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error)
	GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error)
	UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProvider(ctx context.Context, providerID string) error
}

type AgentStore interface {
	CreateAgent(ctx context.Context, agentJSON *models.AgentJSON, officialMeta *models.AgentRegistryExtensions) (*models.AgentResponse, error)
	UpdateAgent(ctx context.Context, agentName, version string, agentJSON *models.AgentJSON) (*models.AgentResponse, error)
	SetAgentStatus(ctx context.Context, agentName, version, status string) (*models.AgentResponse, error)
	ListAgents(ctx context.Context, filter *AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	GetCurrentLatestAgentVersion(ctx context.Context, agentName string) (*models.AgentResponse, error)
	CountAgentVersions(ctx context.Context, agentName string) (int, error)
	CheckAgentVersionExists(ctx context.Context, agentName, version string) (bool, error)
	UnmarkAgentAsLatest(ctx context.Context, agentName string) error
	DeleteAgent(ctx context.Context, agentName, version string) error
	SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *SemanticEmbedding) error
	GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*SemanticEmbeddingMetadata, error)
}

type SkillStore interface {
	CreateSkill(ctx context.Context, skillJSON *models.SkillJSON, officialMeta *models.SkillRegistryExtensions) (*models.SkillResponse, error)
	UpdateSkill(ctx context.Context, skillName, version string, skillJSON *models.SkillJSON) (*models.SkillResponse, error)
	SetSkillStatus(ctx context.Context, skillName, version, status string) (*models.SkillResponse, error)
	ListSkills(ctx context.Context, filter *SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	GetCurrentLatestSkillVersion(ctx context.Context, skillName string) (*models.SkillResponse, error)
	CountSkillVersions(ctx context.Context, skillName string) (int, error)
	CheckSkillVersionExists(ctx context.Context, skillName, version string) (bool, error)
	UnmarkSkillAsLatest(ctx context.Context, skillName string) error
	DeleteSkill(ctx context.Context, skillName, version string) error
}

type PromptStore interface {
	CreatePrompt(ctx context.Context, promptJSON *models.PromptJSON, officialMeta *models.PromptRegistryExtensions) (*models.PromptResponse, error)
	ListPrompts(ctx context.Context, filter *PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error)
	GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error)
	GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error)
	GetCurrentLatestPromptVersion(ctx context.Context, promptName string) (*models.PromptResponse, error)
	CountPromptVersions(ctx context.Context, promptName string) (int, error)
	CheckPromptVersionExists(ctx context.Context, promptName, version string) (bool, error)
	UnmarkPromptAsLatest(ctx context.Context, promptName string) error
	DeletePrompt(ctx context.Context, promptName, version string) error
}

type DeploymentStore interface {
	CreateDeployment(ctx context.Context, deployment *models.Deployment) error
	GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error)
	UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error
	RemoveDeploymentByID(ctx context.Context, id string) error
}

type Store interface {
	ServerStore
	AgentStore
	SkillStore
	PromptStore
	ProviderStore
	DeploymentStore
}

type ServiceDatabase interface {
	Store
	InTransaction(ctx context.Context, fn func(context.Context, Store) error) error
	Close() error
}

type serviceDatabaseAdapter struct {
	storeAdapter
}

func NewServiceDatabase(db Database) ServiceDatabase {
	if db == nil {
		return nil
	}

	return serviceDatabaseAdapter{
		storeAdapter: storeAdapter{db: db},
	}
}

func (s serviceDatabaseAdapter) InTransaction(ctx context.Context, fn func(context.Context, Store) error) error {
	return s.db.InTransaction(ctx, func(txCtx context.Context, tx Transaction) error {
		return fn(txCtx, storeAdapter{db: s.db, tx: tx})
	})
}

func (s serviceDatabaseAdapter) Close() error {
	return s.db.Close()
}

type storeAdapter struct {
	db Database
	tx Transaction
}

func (s storeAdapter) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.db.DeleteServer(ctx, s.tx, serverName, version)
}

func (s storeAdapter) CreateServer(ctx context.Context, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return s.db.CreateServer(ctx, s.tx, serverJSON, officialMeta)
}

func (s storeAdapter) UpdateServer(ctx context.Context, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return s.db.UpdateServer(ctx, s.tx, serverName, version, serverJSON)
}

func (s storeAdapter) SetServerStatus(ctx context.Context, serverName, version, status string) (*apiv0.ServerResponse, error) {
	return s.db.SetServerStatus(ctx, s.tx, serverName, version, status)
}

func (s storeAdapter) ListServers(ctx context.Context, filter *ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.db.ListServers(ctx, s.tx, filter, cursor, limit)
}

func (s storeAdapter) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.db.GetServerByName(ctx, s.tx, serverName)
}

func (s storeAdapter) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	return s.db.GetServerByNameAndVersion(ctx, s.tx, serverName, version)
}

func (s storeAdapter) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.db.GetAllVersionsByServerName(ctx, s.tx, serverName)
}

func (s storeAdapter) GetCurrentLatestVersion(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.db.GetCurrentLatestVersion(ctx, s.tx, serverName)
}

func (s storeAdapter) CountServerVersions(ctx context.Context, serverName string) (int, error) {
	return s.db.CountServerVersions(ctx, s.tx, serverName)
}

func (s storeAdapter) CheckVersionExists(ctx context.Context, serverName, version string) (bool, error) {
	return s.db.CheckVersionExists(ctx, s.tx, serverName, version)
}

func (s storeAdapter) UnmarkAsLatest(ctx context.Context, serverName string) error {
	return s.db.UnmarkAsLatest(ctx, s.tx, serverName)
}

func (s storeAdapter) AcquireServerCreateLock(ctx context.Context, serverName string) error {
	return s.db.AcquireServerCreateLock(ctx, s.tx, serverName)
}

func (s storeAdapter) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *SemanticEmbedding) error {
	return s.db.SetServerEmbedding(ctx, s.tx, serverName, version, embedding)
}

func (s storeAdapter) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*SemanticEmbeddingMetadata, error) {
	return s.db.GetServerEmbeddingMetadata(ctx, s.tx, serverName, version)
}

func (s storeAdapter) UpsertServerReadme(ctx context.Context, readme *ServerReadme) error {
	return s.db.UpsertServerReadme(ctx, s.tx, readme)
}

func (s storeAdapter) GetServerReadme(ctx context.Context, serverName, version string) (*ServerReadme, error) {
	return s.db.GetServerReadme(ctx, s.tx, serverName, version)
}

func (s storeAdapter) GetLatestServerReadme(ctx context.Context, serverName string) (*ServerReadme, error) {
	return s.db.GetLatestServerReadme(ctx, s.tx, serverName)
}

func (s storeAdapter) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.db.CreateProvider(ctx, s.tx, in)
}

func (s storeAdapter) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.db.ListProviders(ctx, s.tx, platform)
}

func (s storeAdapter) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	return s.db.GetProviderByID(ctx, s.tx, providerID)
}

func (s storeAdapter) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.db.UpdateProvider(ctx, s.tx, providerID, in)
}

func (s storeAdapter) DeleteProvider(ctx context.Context, providerID string) error {
	return s.db.DeleteProvider(ctx, s.tx, providerID)
}

func (s storeAdapter) CreateAgent(ctx context.Context, agentJSON *models.AgentJSON, officialMeta *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	return s.db.CreateAgent(ctx, s.tx, agentJSON, officialMeta)
}

func (s storeAdapter) UpdateAgent(ctx context.Context, agentName, version string, agentJSON *models.AgentJSON) (*models.AgentResponse, error) {
	return s.db.UpdateAgent(ctx, s.tx, agentName, version, agentJSON)
}

func (s storeAdapter) SetAgentStatus(ctx context.Context, agentName, version, status string) (*models.AgentResponse, error) {
	return s.db.SetAgentStatus(ctx, s.tx, agentName, version, status)
}

func (s storeAdapter) ListAgents(ctx context.Context, filter *AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.db.ListAgents(ctx, s.tx, filter, cursor, limit)
}

func (s storeAdapter) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.db.GetAgentByName(ctx, s.tx, agentName)
}

func (s storeAdapter) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	return s.db.GetAgentByNameAndVersion(ctx, s.tx, agentName, version)
}

func (s storeAdapter) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.db.GetAllVersionsByAgentName(ctx, s.tx, agentName)
}

func (s storeAdapter) GetCurrentLatestAgentVersion(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.db.GetCurrentLatestAgentVersion(ctx, s.tx, agentName)
}

func (s storeAdapter) CountAgentVersions(ctx context.Context, agentName string) (int, error) {
	return s.db.CountAgentVersions(ctx, s.tx, agentName)
}

func (s storeAdapter) CheckAgentVersionExists(ctx context.Context, agentName, version string) (bool, error) {
	return s.db.CheckAgentVersionExists(ctx, s.tx, agentName, version)
}

func (s storeAdapter) UnmarkAgentAsLatest(ctx context.Context, agentName string) error {
	return s.db.UnmarkAgentAsLatest(ctx, s.tx, agentName)
}

func (s storeAdapter) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.db.DeleteAgent(ctx, s.tx, agentName, version)
}

func (s storeAdapter) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *SemanticEmbedding) error {
	return s.db.SetAgentEmbedding(ctx, s.tx, agentName, version, embedding)
}

func (s storeAdapter) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*SemanticEmbeddingMetadata, error) {
	return s.db.GetAgentEmbeddingMetadata(ctx, s.tx, agentName, version)
}

func (s storeAdapter) CreateSkill(ctx context.Context, skillJSON *models.SkillJSON, officialMeta *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	return s.db.CreateSkill(ctx, s.tx, skillJSON, officialMeta)
}

func (s storeAdapter) UpdateSkill(ctx context.Context, skillName, version string, skillJSON *models.SkillJSON) (*models.SkillResponse, error) {
	return s.db.UpdateSkill(ctx, s.tx, skillName, version, skillJSON)
}

func (s storeAdapter) SetSkillStatus(ctx context.Context, skillName, version, status string) (*models.SkillResponse, error) {
	return s.db.SetSkillStatus(ctx, s.tx, skillName, version, status)
}

func (s storeAdapter) ListSkills(ctx context.Context, filter *SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return s.db.ListSkills(ctx, s.tx, filter, cursor, limit)
}

func (s storeAdapter) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.db.GetSkillByName(ctx, s.tx, skillName)
}

func (s storeAdapter) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.db.GetSkillByNameAndVersion(ctx, s.tx, skillName, version)
}

func (s storeAdapter) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.db.GetAllVersionsBySkillName(ctx, s.tx, skillName)
}

func (s storeAdapter) GetCurrentLatestSkillVersion(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.db.GetCurrentLatestSkillVersion(ctx, s.tx, skillName)
}

func (s storeAdapter) CountSkillVersions(ctx context.Context, skillName string) (int, error) {
	return s.db.CountSkillVersions(ctx, s.tx, skillName)
}

func (s storeAdapter) CheckSkillVersionExists(ctx context.Context, skillName, version string) (bool, error) {
	return s.db.CheckSkillVersionExists(ctx, s.tx, skillName, version)
}

func (s storeAdapter) UnmarkSkillAsLatest(ctx context.Context, skillName string) error {
	return s.db.UnmarkSkillAsLatest(ctx, s.tx, skillName)
}

func (s storeAdapter) DeleteSkill(ctx context.Context, skillName, version string) error {
	return s.db.DeleteSkill(ctx, s.tx, skillName, version)
}

func (s storeAdapter) CreatePrompt(ctx context.Context, promptJSON *models.PromptJSON, officialMeta *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	return s.db.CreatePrompt(ctx, s.tx, promptJSON, officialMeta)
}

func (s storeAdapter) ListPrompts(ctx context.Context, filter *PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return s.db.ListPrompts(ctx, s.tx, filter, cursor, limit)
}

func (s storeAdapter) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.db.GetPromptByName(ctx, s.tx, promptName)
}

func (s storeAdapter) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.db.GetPromptByNameAndVersion(ctx, s.tx, promptName, version)
}

func (s storeAdapter) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.db.GetAllVersionsByPromptName(ctx, s.tx, promptName)
}

func (s storeAdapter) GetCurrentLatestPromptVersion(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.db.GetCurrentLatestPromptVersion(ctx, s.tx, promptName)
}

func (s storeAdapter) CountPromptVersions(ctx context.Context, promptName string) (int, error) {
	return s.db.CountPromptVersions(ctx, s.tx, promptName)
}

func (s storeAdapter) CheckPromptVersionExists(ctx context.Context, promptName, version string) (bool, error) {
	return s.db.CheckPromptVersionExists(ctx, s.tx, promptName, version)
}

func (s storeAdapter) UnmarkPromptAsLatest(ctx context.Context, promptName string) error {
	return s.db.UnmarkPromptAsLatest(ctx, s.tx, promptName)
}

func (s storeAdapter) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.db.DeletePrompt(ctx, s.tx, promptName, version)
}

func (s storeAdapter) CreateDeployment(ctx context.Context, deployment *models.Deployment) error {
	return s.db.CreateDeployment(ctx, s.tx, deployment)
}

func (s storeAdapter) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	return s.db.GetDeployments(ctx, s.tx, filter)
}

func (s storeAdapter) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	return s.db.GetDeploymentByID(ctx, s.tx, id)
}

func (s storeAdapter) UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
	return s.db.UpdateDeploymentState(ctx, s.tx, id, patch)
}

func (s storeAdapter) RemoveDeploymentByID(ctx context.Context, id string) error {
	return s.db.RemoveDeploymentByID(ctx, s.tx, id)
}

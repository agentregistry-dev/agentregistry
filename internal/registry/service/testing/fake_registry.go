// Package testing provides test utilities for the registry service.
package testing

import (
	"context"
	"sync"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// FakeRegistry is a configurable fake implementation of service.RegistryService for testing.
// It supports both data-driven setup via struct fields and function hooks for custom behavior.
type FakeRegistry struct {
	mu sync.Mutex

	// Data fields for simple data-driven tests
	Servers      []*apiv0.ServerResponse
	Agents       []*models.AgentResponse
	Skills       []*models.SkillResponse
	Deployments  []*models.Deployment
	ServerReadme *database.ServerReadme

	// Embedding metadata maps (keyed by "name@version")
	ServerEmbeddingMeta map[string]*database.SemanticEmbeddingMetadata
	AgentEmbeddingMeta  map[string]*database.SemanticEmbeddingMetadata

	// Call counters for verification
	UpsertServerEmbeddingCalls int
	UpsertAgentEmbeddingCalls  int

	// Function hooks for custom behavior (take precedence over data fields when set)
	ListServersFn                   func(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByNameFn               func(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	GetServerByNameAndVersionFn     func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerNameFn    func(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	CreateServerFn                  func(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	UpdateServerFn                  func(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	StoreServerReadmeFn             func(ctx context.Context, serverName, version string, content []byte, contentType string) error
	GetServerReadmeLatestFn         func(ctx context.Context, serverName string) (*database.ServerReadme, error)
	GetServerReadmeByVersionFn      func(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	DeleteServerFn                  func(ctx context.Context, serverName, version string) error
	UpsertServerEmbeddingFn         func(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error
	GetServerEmbeddingMetadataFn    func(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error)
	ListAgentsFn                    func(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByNameFn                func(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersionFn      func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	GetAllVersionsByAgentNameFn     func(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	CreateAgentFn                   func(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	DeleteAgentFn                   func(ctx context.Context, agentName, version string) error
	UpsertAgentEmbeddingFn          func(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error
	GetAgentEmbeddingMetadataFn     func(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error)
	ListSkillsFn                    func(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByNameFn                func(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersionFn      func(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	GetAllVersionsBySkillNameFn     func(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	CreateSkillFn                   func(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
	GetDeploymentsFn                func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeploymentByNameAndVersionFn func(ctx context.Context, resourceName, version, artifactType string) (*models.Deployment, error)
	DeployServerFn                  func(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error)
	DeployAgentFn                   func(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error)
	UpdateDeploymentConfigFn        func(ctx context.Context, resourceName, version, artifactType string, config map[string]string) (*models.Deployment, error)
	RemoveDeploymentFn              func(ctx context.Context, resourceName, version, artifactType string) error
	ReconcileAllFn                  func(ctx context.Context) error
}

// NewFakeRegistry creates a new FakeRegistry with initialized maps.
func NewFakeRegistry() *FakeRegistry {
	return &FakeRegistry{
		ServerEmbeddingMeta: make(map[string]*database.SemanticEmbeddingMetadata),
		AgentEmbeddingMeta:  make(map[string]*database.SemanticEmbeddingMetadata),
	}
}

// Server methods

func (f *FakeRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.ListServersFn != nil {
		return f.ListServersFn(ctx, filter, cursor, limit)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if cursor != "" {
		return nil, "", nil
	}
	return f.Servers, "", nil
}

func (f *FakeRegistry) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameFn != nil {
		return f.GetServerByNameFn(ctx, serverName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Servers) > 0 {
		return f.Servers[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameAndVersionFn != nil {
		return f.GetServerByNameAndVersionFn(ctx, serverName, version)
	}
	return f.GetServerByName(ctx, serverName)
}

func (f *FakeRegistry) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if f.GetAllVersionsByServerNameFn != nil {
		return f.GetAllVersionsByServerNameFn(ctx, serverName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Servers, nil
}

func (f *FakeRegistry) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if f.CreateServerFn != nil {
		return f.CreateServerFn(ctx, req)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	if f.UpdateServerFn != nil {
		return f.UpdateServerFn(ctx, serverName, version, req, newStatus)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if f.StoreServerReadmeFn != nil {
		return f.StoreServerReadmeFn(ctx, serverName, version, content, contentType)
	}
	return database.ErrNotFound
}

func (f *FakeRegistry) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if f.GetServerReadmeLatestFn != nil {
		return f.GetServerReadmeLatestFn(ctx, serverName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ServerReadme != nil {
		return f.ServerReadme, nil
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if f.GetServerReadmeByVersionFn != nil {
		return f.GetServerReadmeByVersionFn(ctx, serverName, version)
	}
	return f.GetServerReadmeLatest(ctx, serverName)
}

func (f *FakeRegistry) DeleteServer(ctx context.Context, serverName, version string) error {
	if f.DeleteServerFn != nil {
		return f.DeleteServerFn(ctx, serverName, version)
	}
	return database.ErrNotFound
}

func (f *FakeRegistry) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	if f.UpsertServerEmbeddingFn != nil {
		return f.UpsertServerEmbeddingFn(ctx, serverName, version, embedding)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpsertServerEmbeddingCalls++
	return nil
}

func (f *FakeRegistry) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	if f.GetServerEmbeddingMetadataFn != nil {
		return f.GetServerEmbeddingMetadataFn(ctx, serverName, version)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := serverName + "@" + version
	if meta, ok := f.ServerEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

// Agent methods

func (f *FakeRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.ListAgentsFn != nil {
		return f.ListAgentsFn(ctx, filter, cursor, limit)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if cursor != "" {
		return nil, "", nil
	}
	return f.Agents, "", nil
}

func (f *FakeRegistry) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.GetAgentByNameFn != nil {
		return f.GetAgentByNameFn(ctx, agentName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Agents) > 0 {
		return f.Agents[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.GetAgentByNameAndVersionFn != nil {
		return f.GetAgentByNameAndVersionFn(ctx, agentName, version)
	}
	return f.GetAgentByName(ctx, agentName)
}

func (f *FakeRegistry) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if f.GetAllVersionsByAgentNameFn != nil {
		return f.GetAllVersionsByAgentNameFn(ctx, agentName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Agents, nil
}

func (f *FakeRegistry) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	if f.CreateAgentFn != nil {
		return f.CreateAgentFn(ctx, req)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) DeleteAgent(ctx context.Context, agentName, version string) error {
	if f.DeleteAgentFn != nil {
		return f.DeleteAgentFn(ctx, agentName, version)
	}
	return database.ErrNotFound
}

func (f *FakeRegistry) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	if f.UpsertAgentEmbeddingFn != nil {
		return f.UpsertAgentEmbeddingFn(ctx, agentName, version, embedding)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.UpsertAgentEmbeddingCalls++
	return nil
}

func (f *FakeRegistry) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	if f.GetAgentEmbeddingMetadataFn != nil {
		return f.GetAgentEmbeddingMetadataFn(ctx, agentName, version)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	key := agentName + "@" + version
	if meta, ok := f.AgentEmbeddingMeta[key]; ok {
		return meta, nil
	}
	return nil, database.ErrNotFound
}

// Skill methods

func (f *FakeRegistry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if f.ListSkillsFn != nil {
		return f.ListSkillsFn(ctx, filter, cursor, limit)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Skills, "", nil
}

func (f *FakeRegistry) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if f.GetSkillByNameFn != nil {
		return f.GetSkillByNameFn(ctx, skillName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.Skills) > 0 {
		return f.Skills[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if f.GetSkillByNameAndVersionFn != nil {
		return f.GetSkillByNameAndVersionFn(ctx, skillName, version)
	}
	return f.GetSkillByName(ctx, skillName)
}

func (f *FakeRegistry) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if f.GetAllVersionsBySkillNameFn != nil {
		return f.GetAllVersionsBySkillNameFn(ctx, skillName)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Skills, nil
}

func (f *FakeRegistry) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	if f.CreateSkillFn != nil {
		return f.CreateSkillFn(ctx, req)
	}
	return nil, database.ErrNotFound
}

// Deployment methods

func (f *FakeRegistry) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.GetDeploymentsFn != nil {
		return f.GetDeploymentsFn(ctx, filter)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Deployments, nil
}

func (f *FakeRegistry) GetDeploymentByNameAndVersion(ctx context.Context, resourceName, version, artifactType string) (*models.Deployment, error) {
	if f.GetDeploymentByNameAndVersionFn != nil {
		return f.GetDeploymentByNameAndVersionFn(ctx, resourceName, version, artifactType)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
	if f.DeployServerFn != nil {
		return f.DeployServerFn(ctx, serverName, version, config, preferRemote, runtime)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
	if f.DeployAgentFn != nil {
		return f.DeployAgentFn(ctx, agentName, version, config, preferRemote, runtime)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) UpdateDeploymentConfig(ctx context.Context, resourceName, version, artifactType string, config map[string]string) (*models.Deployment, error) {
	if f.UpdateDeploymentConfigFn != nil {
		return f.UpdateDeploymentConfigFn(ctx, resourceName, version, artifactType, config)
	}
	return nil, database.ErrNotFound
}

func (f *FakeRegistry) RemoveDeployment(ctx context.Context, resourceName, version, artifactType string) error {
	if f.RemoveDeploymentFn != nil {
		return f.RemoveDeploymentFn(ctx, resourceName, version, artifactType)
	}
	return database.ErrNotFound
}

func (f *FakeRegistry) ReconcileAll(ctx context.Context) error {
	if f.ReconcileAllFn != nil {
		return f.ReconcileAllFn(ctx)
	}
	return nil
}

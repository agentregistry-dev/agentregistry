package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/internal/registry/api/router"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"go.opentelemetry.io/otel/metric/noop"
)

type fakeClientRegistry struct {
	Servers []*apiv0.ServerResponse
	Agents  []*models.AgentResponse
	Skills  []*models.SkillResponse
	Prompts []*models.PromptResponse

	ListServersFn                func(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	GetServerByNameFn            func(ctx context.Context, serverName string) (*apiv0.ServerResponse, error)
	GetServerByNameAndVersionFn  func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAllVersionsByServerNameFn func(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	CreateServerFn               func(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error)
	UpdateServerFn               func(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error)
	StoreServerReadmeFn          func(ctx context.Context, serverName, version string, content []byte, contentType string) error
	GetServerReadmeLatestFn      func(ctx context.Context, serverName string) (*database.ServerReadme, error)
	GetServerReadmeByVersionFn   func(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	DeleteServerFn               func(ctx context.Context, serverName, version string) error

	ListAgentsFn                func(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	GetAgentByNameFn            func(ctx context.Context, agentName string) (*models.AgentResponse, error)
	GetAgentByNameAndVersionFn  func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	GetAllVersionsByAgentNameFn func(ctx context.Context, agentName string) ([]*models.AgentResponse, error)
	CreateAgentFn               func(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error)
	DeleteAgentFn               func(ctx context.Context, agentName, version string) error

	ListSkillsFn                func(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	GetSkillByNameFn            func(ctx context.Context, skillName string) (*models.SkillResponse, error)
	GetSkillByNameAndVersionFn  func(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	GetAllVersionsBySkillNameFn func(ctx context.Context, skillName string) ([]*models.SkillResponse, error)
	CreateSkillFn               func(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error)
	DeleteSkillFn               func(ctx context.Context, skillName, version string) error

	ListPromptsFn                func(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error)
	GetPromptByNameFn            func(ctx context.Context, promptName string) (*models.PromptResponse, error)
	GetPromptByNameAndVersionFn  func(ctx context.Context, promptName, version string) (*models.PromptResponse, error)
	GetAllVersionsByPromptNameFn func(ctx context.Context, promptName string) ([]*models.PromptResponse, error)
	CreatePromptFn               func(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error)
	DeletePromptFn               func(ctx context.Context, promptName, version string) error

	ListProvidersFn   func(ctx context.Context, platform *string) ([]*models.Provider, error)
	GetProviderByIDFn func(ctx context.Context, providerID string) (*models.Provider, error)
	CreateProviderFn  func(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error)
	UpdateProviderFn  func(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error)
	DeleteProviderFn  func(ctx context.Context, providerID string) error

	GetDeploymentsFn       func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	GetDeploymentByIDFn    func(ctx context.Context, id string) (*models.Deployment, error)
	DeployServerFn         func(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	DeployAgentFn          func(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	RemoveDeploymentByIDFn func(ctx context.Context, id string) error
	CreateDeploymentFn     func(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	UndeployDeploymentFn   func(ctx context.Context, deployment *models.Deployment) error
	GetDeploymentLogsFn    func(ctx context.Context, deployment *models.Deployment) ([]string, error)
	CancelDeploymentFn     func(ctx context.Context, deployment *models.Deployment) error
}

func newFakeClientRegistry() *fakeClientRegistry { return &fakeClientRegistry{} }

func (f *fakeClientRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.ListServersFn != nil {
		return f.ListServersFn(ctx, filter, cursor, limit)
	}
	if cursor != "" {
		return nil, "", nil
	}
	return f.Servers, "", nil
}
func (f *fakeClientRegistry) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameFn != nil {
		return f.GetServerByNameFn(ctx, serverName)
	}
	if len(f.Servers) > 0 {
		return f.Servers[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameAndVersionFn != nil {
		return f.GetServerByNameAndVersionFn(ctx, serverName, version)
	}
	return f.GetServerByName(ctx, serverName)
}
func (f *fakeClientRegistry) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if f.GetAllVersionsByServerNameFn != nil {
		return f.GetAllVersionsByServerNameFn(ctx, serverName)
	}
	return f.Servers, nil
}
func (f *fakeClientRegistry) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if f.CreateServerFn != nil {
		return f.CreateServerFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	if f.UpdateServerFn != nil {
		return f.UpdateServerFn(ctx, serverName, version, req, newStatus)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	if f.StoreServerReadmeFn != nil {
		return f.StoreServerReadmeFn(ctx, serverName, version, content, contentType)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if f.GetServerReadmeLatestFn != nil {
		return f.GetServerReadmeLatestFn(ctx, serverName)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if f.GetServerReadmeByVersionFn != nil {
		return f.GetServerReadmeByVersionFn(ctx, serverName, version)
	}
	return f.GetServerReadmeLatest(ctx, serverName)
}
func (f *fakeClientRegistry) DeleteServer(ctx context.Context, serverName, version string) error {
	if f.DeleteServerFn != nil {
		return f.DeleteServerFn(ctx, serverName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return nil
}
func (f *fakeClientRegistry) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.ListAgentsFn != nil {
		return f.ListAgentsFn(ctx, filter, cursor, limit)
	}
	if cursor != "" {
		return nil, "", nil
	}
	return f.Agents, "", nil
}
func (f *fakeClientRegistry) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.GetAgentByNameFn != nil {
		return f.GetAgentByNameFn(ctx, agentName)
	}
	if len(f.Agents) > 0 {
		return f.Agents[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.GetAgentByNameAndVersionFn != nil {
		return f.GetAgentByNameAndVersionFn(ctx, agentName, version)
	}
	return f.GetAgentByName(ctx, agentName)
}
func (f *fakeClientRegistry) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if f.GetAllVersionsByAgentNameFn != nil {
		return f.GetAllVersionsByAgentNameFn(ctx, agentName)
	}
	return f.Agents, nil
}
func (f *fakeClientRegistry) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	if f.CreateAgentFn != nil {
		return f.CreateAgentFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return nil, nil
}
func (f *fakeClientRegistry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return nil, nil
}
func (f *fakeClientRegistry) DeleteAgent(ctx context.Context, agentName, version string) error {
	if f.DeleteAgentFn != nil {
		return f.DeleteAgentFn(ctx, agentName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return nil
}
func (f *fakeClientRegistry) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if f.ListSkillsFn != nil {
		return f.ListSkillsFn(ctx, filter, cursor, limit)
	}
	return f.Skills, "", nil
}
func (f *fakeClientRegistry) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if f.GetSkillByNameFn != nil {
		return f.GetSkillByNameFn(ctx, skillName)
	}
	if len(f.Skills) > 0 {
		return f.Skills[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if f.GetSkillByNameAndVersionFn != nil {
		return f.GetSkillByNameAndVersionFn(ctx, skillName, version)
	}
	return f.GetSkillByName(ctx, skillName)
}
func (f *fakeClientRegistry) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if f.GetAllVersionsBySkillNameFn != nil {
		return f.GetAllVersionsBySkillNameFn(ctx, skillName)
	}
	return f.Skills, nil
}
func (f *fakeClientRegistry) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	if f.CreateSkillFn != nil {
		return f.CreateSkillFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeleteSkill(ctx context.Context, skillName, version string) error {
	if f.DeleteSkillFn != nil {
		return f.DeleteSkillFn(ctx, skillName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if f.ListPromptsFn != nil {
		return f.ListPromptsFn(ctx, filter, cursor, limit)
	}
	return f.Prompts, "", nil
}
func (f *fakeClientRegistry) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	if f.GetPromptByNameFn != nil {
		return f.GetPromptByNameFn(ctx, promptName)
	}
	if len(f.Prompts) > 0 {
		return f.Prompts[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	if f.GetPromptByNameAndVersionFn != nil {
		return f.GetPromptByNameAndVersionFn(ctx, promptName, version)
	}
	return f.GetPromptByName(ctx, promptName)
}
func (f *fakeClientRegistry) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	if f.GetAllVersionsByPromptNameFn != nil {
		return f.GetAllVersionsByPromptNameFn(ctx, promptName)
	}
	return f.Prompts, nil
}
func (f *fakeClientRegistry) CreatePrompt(ctx context.Context, req *models.PromptJSON) (*models.PromptResponse, error) {
	if f.CreatePromptFn != nil {
		return f.CreatePromptFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeletePrompt(ctx context.Context, promptName, version string) error {
	if f.DeletePromptFn != nil {
		return f.DeletePromptFn(ctx, promptName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	if f.ListProvidersFn != nil {
		return f.ListProvidersFn(ctx, platform)
	}
	return nil, nil
}
func (f *fakeClientRegistry) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.GetProviderByIDFn != nil {
		return f.GetProviderByIDFn(ctx, providerID)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	if f.CreateProviderFn != nil {
		return f.CreateProviderFn(ctx, in)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	if f.UpdateProviderFn != nil {
		return f.UpdateProviderFn(ctx, providerID, in)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeleteProvider(ctx context.Context, providerID string) error {
	if f.DeleteProviderFn != nil {
		return f.DeleteProviderFn(ctx, providerID)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.GetDeploymentsFn != nil {
		return f.GetDeploymentsFn(ctx, filter)
	}
	return nil, nil
}
func (f *fakeClientRegistry) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	if f.GetDeploymentByIDFn != nil {
		return f.GetDeploymentByIDFn(ctx, id)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.DeployServerFn != nil {
		return f.DeployServerFn(ctx, serverName, version, config, preferRemote, providerID)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.DeployAgentFn != nil {
		return f.DeployAgentFn(ctx, agentName, version, config, preferRemote, providerID)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) RemoveDeploymentByID(ctx context.Context, id string) error {
	if f.RemoveDeploymentByIDFn != nil {
		return f.RemoveDeploymentByIDFn(ctx, id)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	if f.CreateDeploymentFn != nil {
		return f.CreateDeploymentFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	if f.UndeployDeploymentFn != nil {
		return f.UndeployDeploymentFn(ctx, deployment)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if f.GetDeploymentLogsFn != nil {
		return f.GetDeploymentLogsFn(ctx, deployment)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	if f.CancelDeploymentFn != nil {
		return f.CancelDeploymentFn(ctx, deployment)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) ReconcileAll(ctx context.Context) error { return nil }

type fakeClientServerStore struct{ registry *fakeClientRegistry }

func (s *fakeClientServerStore) DeleteServer(ctx context.Context, serverName, version string) error {
	return s.registry.DeleteServer(ctx, serverName, version)
}

func (s *fakeClientServerStore) CreateServer(ctx context.Context, req *apiv0.ServerJSON, _ *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return s.registry.CreateServer(ctx, req)
}

func (s *fakeClientServerStore) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return s.registry.UpdateServer(ctx, serverName, version, req, nil)
}

func (s *fakeClientServerStore) SetServerStatus(context.Context, string, string, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeClientServerStore) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	return s.registry.ListServers(ctx, filter, cursor, limit)
}

func (s *fakeClientServerStore) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	server, err := s.registry.GetServerByName(ctx, serverName)
	if err == nil {
		return server, nil
	}
	return &apiv0.ServerResponse{Server: apiv0.ServerJSON{Name: serverName, Version: "latest"}}, nil
}

func (s *fakeClientServerStore) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	server, err := s.registry.GetServerByNameAndVersion(ctx, serverName, version)
	if err == nil {
		return server, nil
	}
	return &apiv0.ServerResponse{Server: apiv0.ServerJSON{Name: serverName, Version: version}}, nil
}

func (s *fakeClientServerStore) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	return s.registry.GetAllVersionsByServerName(ctx, serverName)
}

func (s *fakeClientServerStore) GetCurrentLatestVersion(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return s.registry.GetServerByName(ctx, serverName)
}

func (s *fakeClientServerStore) CountServerVersions(_ context.Context, serverName string) (int, error) {
	count := 0
	for _, server := range s.registry.Servers {
		if server.Server.Name == serverName {
			count++
		}
	}
	return count, nil
}

func (s *fakeClientServerStore) CheckVersionExists(_ context.Context, serverName, version string) (bool, error) {
	for _, server := range s.registry.Servers {
		if server.Server.Name == serverName && server.Server.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeClientServerStore) UnmarkAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeClientServerStore) AcquireServerCreateLock(context.Context, string) error {
	return nil
}

func (s *fakeClientServerStore) SetServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (s *fakeClientServerStore) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (s *fakeClientServerStore) UpsertServerReadme(ctx context.Context, readme *database.ServerReadme) error {
	if readme == nil {
		return nil
	}
	return s.registry.StoreServerReadme(ctx, readme.ServerName, readme.Version, readme.Content, readme.ContentType)
}

func (s *fakeClientServerStore) GetServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	return s.registry.GetServerReadmeByVersion(ctx, serverName, version)
}

func (s *fakeClientServerStore) GetLatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	return s.registry.GetServerReadmeLatest(ctx, serverName)
}

type fakeClientAgentStore struct{ registry *fakeClientRegistry }

func (s *fakeClientAgentStore) CreateAgent(ctx context.Context, req *models.AgentJSON, _ *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	return s.registry.CreateAgent(ctx, req)
}

func (s *fakeClientAgentStore) UpdateAgent(context.Context, string, string, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeClientAgentStore) SetAgentStatus(context.Context, string, string, string) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeClientAgentStore) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	return s.registry.ListAgents(ctx, filter, cursor, limit)
}

func (s *fakeClientAgentStore) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	agent, err := s.registry.GetAgentByName(ctx, agentName)
	if err == nil {
		return agent, nil
	}
	return &models.AgentResponse{Agent: models.AgentJSON{AgentManifest: models.AgentManifest{Name: agentName}, Version: "latest"}}, nil
}

func (s *fakeClientAgentStore) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	agent, err := s.registry.GetAgentByNameAndVersion(ctx, agentName, version)
	if err == nil {
		return agent, nil
	}
	return &models.AgentResponse{Agent: models.AgentJSON{AgentManifest: models.AgentManifest{Name: agentName}, Version: version}}, nil
}

func (s *fakeClientAgentStore) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	return s.registry.GetAllVersionsByAgentName(ctx, agentName)
}

func (s *fakeClientAgentStore) GetCurrentLatestAgentVersion(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return s.registry.GetAgentByName(ctx, agentName)
}

func (s *fakeClientAgentStore) CountAgentVersions(_ context.Context, agentName string) (int, error) {
	count := 0
	for _, agent := range s.registry.Agents {
		if agent.Agent.Name == agentName {
			count++
		}
	}
	return count, nil
}

func (s *fakeClientAgentStore) CheckAgentVersionExists(_ context.Context, agentName, version string) (bool, error) {
	for _, agent := range s.registry.Agents {
		if agent.Agent.Name == agentName && agent.Agent.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeClientAgentStore) UnmarkAgentAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeClientAgentStore) DeleteAgent(ctx context.Context, agentName, version string) error {
	return s.registry.DeleteAgent(ctx, agentName, version)
}

func (s *fakeClientAgentStore) SetAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (s *fakeClientAgentStore) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

type fakeClientSkillStore struct{ registry *fakeClientRegistry }

func (s *fakeClientSkillStore) CreateSkill(ctx context.Context, req *models.SkillJSON, _ *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	return s.registry.CreateSkill(ctx, req)
}

func (s *fakeClientSkillStore) UpdateSkill(context.Context, string, string, *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeClientSkillStore) SetSkillStatus(context.Context, string, string, string) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (s *fakeClientSkillStore) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	return s.registry.ListSkills(ctx, filter, cursor, limit)
}

func (s *fakeClientSkillStore) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.registry.GetSkillByName(ctx, skillName)
}

func (s *fakeClientSkillStore) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	return s.registry.GetSkillByNameAndVersion(ctx, skillName, version)
}

func (s *fakeClientSkillStore) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	return s.registry.GetAllVersionsBySkillName(ctx, skillName)
}

func (s *fakeClientSkillStore) GetCurrentLatestSkillVersion(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return s.registry.GetSkillByName(ctx, skillName)
}

func (s *fakeClientSkillStore) CountSkillVersions(_ context.Context, skillName string) (int, error) {
	count := 0
	for _, skill := range s.registry.Skills {
		if skill.Skill.Name == skillName {
			count++
		}
	}
	return count, nil
}

func (s *fakeClientSkillStore) CheckSkillVersionExists(_ context.Context, skillName, version string) (bool, error) {
	for _, skill := range s.registry.Skills {
		if skill.Skill.Name == skillName && skill.Skill.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeClientSkillStore) UnmarkSkillAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeClientSkillStore) DeleteSkill(ctx context.Context, skillName, version string) error {
	return s.registry.DeleteSkill(ctx, skillName, version)
}

type fakeClientPromptStore struct{ registry *fakeClientRegistry }

func (s *fakeClientPromptStore) CreatePrompt(ctx context.Context, req *models.PromptJSON, _ *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	return s.registry.CreatePrompt(ctx, req)
}

func (s *fakeClientPromptStore) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	return s.registry.ListPrompts(ctx, filter, cursor, limit)
}

func (s *fakeClientPromptStore) GetPromptByName(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.registry.GetPromptByName(ctx, promptName)
}

func (s *fakeClientPromptStore) GetPromptByNameAndVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	return s.registry.GetPromptByNameAndVersion(ctx, promptName, version)
}

func (s *fakeClientPromptStore) GetAllVersionsByPromptName(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	return s.registry.GetAllVersionsByPromptName(ctx, promptName)
}

func (s *fakeClientPromptStore) GetCurrentLatestPromptVersion(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return s.registry.GetPromptByName(ctx, promptName)
}

func (s *fakeClientPromptStore) CountPromptVersions(_ context.Context, promptName string) (int, error) {
	count := 0
	for _, prompt := range s.registry.Prompts {
		if prompt.Prompt.Name == promptName {
			count++
		}
	}
	return count, nil
}

func (s *fakeClientPromptStore) CheckPromptVersionExists(_ context.Context, promptName, version string) (bool, error) {
	for _, prompt := range s.registry.Prompts {
		if prompt.Prompt.Name == promptName && prompt.Prompt.Version == version {
			return true, nil
		}
	}
	return false, nil
}

func (s *fakeClientPromptStore) UnmarkPromptAsLatest(context.Context, string) error {
	return nil
}

func (s *fakeClientPromptStore) DeletePrompt(ctx context.Context, promptName, version string) error {
	return s.registry.DeletePrompt(ctx, promptName, version)
}

type fakeClientProviderStore struct{ registry *fakeClientRegistry }

func (s *fakeClientProviderStore) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	return s.registry.CreateProvider(ctx, in)
}

func (s *fakeClientProviderStore) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	return s.registry.ListProviders(ctx, platform)
}

func (s *fakeClientProviderStore) GetProviderByID(ctx context.Context, providerID string) (*models.Provider, error) {
	if s.registry.GetProviderByIDFn != nil {
		return s.registry.GetProviderByID(ctx, providerID)
	}
	return &models.Provider{ID: providerID, Name: "Local provider", Platform: "local"}, nil
}

func (s *fakeClientProviderStore) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	return s.registry.UpdateProvider(ctx, providerID, in)
}

func (s *fakeClientProviderStore) DeleteProvider(ctx context.Context, providerID string) error {
	return s.registry.DeleteProvider(ctx, providerID)
}

type fakeClientDeploymentStore struct {
	registry         *fakeClientRegistry
	deployments      map[string]*models.Deployment
	nextDeploymentID int
}

func newFakeClientDeploymentStore(registry *fakeClientRegistry) *fakeClientDeploymentStore {
	return &fakeClientDeploymentStore{registry: registry, deployments: map[string]*models.Deployment{}, nextDeploymentID: 1}
}

func (s *fakeClientDeploymentStore) CreateDeployment(ctx context.Context, req *models.Deployment) error {
	created := req
	if s.registry.CreateDeploymentFn != nil {
		var err error
		created, err = s.registry.CreateDeploymentFn(ctx, req)
		if err != nil {
			return err
		}
	}
	if created == nil {
		created = req
	}
	stored := *created
	if stored.ID == "" {
		stored.ID = "dep-created-" + strconv.Itoa(s.nextDeploymentID)
		s.nextDeploymentID++
	}
	req.ID = stored.ID
	s.deployments[stored.ID] = &stored
	return nil
}

func (s *fakeClientDeploymentStore) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if s.registry.GetDeploymentsFn != nil {
		return s.registry.GetDeploymentsFn(ctx, filter)
	}
	deployments := make([]*models.Deployment, 0, len(s.deployments))
	for _, deployment := range s.deployments {
		deployments = append(deployments, deployment)
	}
	return deployments, nil
}

func (s *fakeClientDeploymentStore) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	if s.registry.GetDeploymentByIDFn != nil {
		return s.registry.GetDeploymentByIDFn(ctx, id)
	}
	if deployment, ok := s.deployments[id]; ok {
		return deployment, nil
	}
	return nil, database.ErrNotFound
}

func (s *fakeClientDeploymentStore) UpdateDeploymentState(_ context.Context, id string, patch *models.DeploymentStatePatch) error {
	deployment, ok := s.deployments[id]
	if !ok {
		return database.ErrNotFound
	}
	if patch.Status != nil {
		deployment.Status = *patch.Status
	}
	if patch.Error != nil {
		deployment.Error = *patch.Error
	}
	return nil
}

func (s *fakeClientDeploymentStore) RemoveDeploymentByID(ctx context.Context, id string) error {
	if s.registry.RemoveDeploymentByIDFn != nil {
		return s.registry.RemoveDeploymentByIDFn(ctx, id)
	}
	delete(s.deployments, id)
	return nil
}

type fakeClientDeploymentAdapter struct{ registry *fakeClientRegistry }

func (a *fakeClientDeploymentAdapter) Platform() string { return "local" }

func (a *fakeClientDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (a *fakeClientDeploymentAdapter) Deploy(ctx context.Context, deployment *models.Deployment) (*models.DeploymentActionResult, error) {
	if deployment == nil {
		return nil, database.ErrInvalidInput
	}
	if deployment.ResourceType == "agent" {
		if a.registry.DeployAgentFn != nil {
			result, err := a.registry.DeployAgentFn(ctx, deployment.ServerName, deployment.Version, deployment.Env, deployment.PreferRemote, deployment.ProviderID)
			if err != nil {
				return nil, err
			}
			if result != nil && result.Status != "" {
				return &models.DeploymentActionResult{Status: result.Status}, nil
			}
		}
	} else if a.registry.DeployServerFn != nil {
		result, err := a.registry.DeployServerFn(ctx, deployment.ServerName, deployment.Version, deployment.Env, deployment.PreferRemote, deployment.ProviderID)
		if err != nil {
			return nil, err
		}
		if result != nil && result.Status != "" {
			return &models.DeploymentActionResult{Status: result.Status}, nil
		}
	}
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}

func (a *fakeClientDeploymentAdapter) Undeploy(context.Context, *models.Deployment) error {
	return nil
}

func (a *fakeClientDeploymentAdapter) GetLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	if a.registry.GetDeploymentLogsFn != nil {
		return a.registry.GetDeploymentLogsFn(ctx, deployment)
	}
	return nil, database.ErrNotFound
}

func (a *fakeClientDeploymentAdapter) Cancel(ctx context.Context, deployment *models.Deployment) error {
	if a.registry.CancelDeploymentFn != nil {
		return a.registry.CancelDeploymentFn(ctx, deployment)
	}
	return database.ErrNotFound
}

func (a *fakeClientDeploymentAdapter) Discover(context.Context, string) ([]*models.Deployment, error) {
	return nil, nil
}

type fakeClientStore struct {
	*fakeClientServerStore
	*fakeClientAgentStore
	*fakeClientSkillStore
	*fakeClientPromptStore
	*fakeClientProviderStore
	*fakeClientDeploymentStore
}

func newFakeClientStore(registry *fakeClientRegistry) *fakeClientStore {
	return &fakeClientStore{
		fakeClientServerStore:     &fakeClientServerStore{registry: registry},
		fakeClientAgentStore:      &fakeClientAgentStore{registry: registry},
		fakeClientSkillStore:      &fakeClientSkillStore{registry: registry},
		fakeClientPromptStore:     &fakeClientPromptStore{registry: registry},
		fakeClientProviderStore:   &fakeClientProviderStore{registry: registry},
		fakeClientDeploymentStore: newFakeClientDeploymentStore(registry),
	}
}

func (s *fakeClientStore) Servers() database.ServerStore { return s.fakeClientServerStore }

func (s *fakeClientStore) Providers() database.ProviderStore { return s.fakeClientProviderStore }

func (s *fakeClientStore) Agents() database.AgentStore { return s.fakeClientAgentStore }

func (s *fakeClientStore) Skills() database.SkillStore { return s.fakeClientSkillStore }

func (s *fakeClientStore) Prompts() database.PromptStore { return s.fakeClientPromptStore }

func (s *fakeClientStore) Deployments() database.DeploymentStore { return s.fakeClientDeploymentStore }

func (s *fakeClientStore) InTransaction(ctx context.Context, fn func(context.Context, database.Scope) error) error {
	return fn(ctx, s)
}

func (s *fakeClientStore) Close() error {
	return nil
}

func TestClientIntegration_PingAndVersion(t *testing.T) {
	fake := newFakeClientRegistry()
	client, cleanup := newClientWithInProcessServer(t, fake)
	defer cleanup()

	if err := client.Ping(); err != nil {
		t.Fatalf("Ping() failed: %v", err)
	}

	version, err := client.GetVersion()
	if err != nil {
		t.Fatalf("GetVersion() failed: %v", err)
	}
	if version.Version != "test-version" {
		t.Fatalf("GetVersion() returned unexpected version: got %q", version.Version)
	}
	if version.GitCommit != "test-commit" {
		t.Fatalf("GetVersion() returned unexpected git commit: got %q", version.GitCommit)
	}
}

func TestClientIntegration_CatalogRoutes_HappyPath(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	serverV1 := &apiv0.ServerResponse{
		Server: apiv0.ServerJSON{
			Name:        "acme/weather",
			Description: "Weather MCP server",
			Version:     "1.0.0",
		},
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.StatusActive,
				PublishedAt: now,
				UpdatedAt:   now,
				IsLatest:    false,
			},
		},
	}
	serverV2 := &apiv0.ServerResponse{
		Server: apiv0.ServerJSON{
			Name:        "acme/weather",
			Description: "Weather MCP server",
			Version:     "2.0.0",
		},
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.StatusActive,
				PublishedAt: now,
				UpdatedAt:   now,
				IsLatest:    true,
			},
		},
	}
	skillV1 := &models.SkillResponse{
		Skill: models.SkillJSON{
			Name:        "acme/translate",
			Description: "Translate text",
			Version:     "1.0.0",
		},
	}
	agentV1 := &models.AgentResponse{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:        "acme/planner",
				Description: "Planning agent",
				Version:     "1.0.0",
			},
			Version: "1.0.0",
		},
	}

	var deletedAgent bool
	var deletedServer bool

	fake := newFakeClientRegistry()
	fake.ListServersFn = func(_ context.Context, _ *database.ServerFilter, _ string, _ int) ([]*apiv0.ServerResponse, string, error) {
		return []*apiv0.ServerResponse{serverV1, serverV2}, "", nil
	}
	fake.GetAllVersionsByServerNameFn = func(_ context.Context, _ string) ([]*apiv0.ServerResponse, error) {
		return []*apiv0.ServerResponse{serverV1, serverV2}, nil
	}
	fake.GetServerByNameAndVersionFn = func(_ context.Context, _ string, version string) (*apiv0.ServerResponse, error) {
		if version == "2.0.0" {
			return serverV2, nil
		}
		return serverV1, nil
	}
	fake.CreateServerFn = func(_ context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
		return &apiv0.ServerResponse{
			Server: *req,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.StatusActive,
					PublishedAt: now,
					UpdatedAt:   now,
					IsLatest:    true,
				},
			},
		}, nil
	}
	fake.DeleteServerFn = func(_ context.Context, _, _ string) error {
		deletedServer = true
		return nil
	}
	fake.ListSkillsFn = func(_ context.Context, _ *database.SkillFilter, _ string, _ int) ([]*models.SkillResponse, string, error) {
		return []*models.SkillResponse{skillV1}, "", nil
	}
	fake.GetSkillByNameFn = func(_ context.Context, _ string) (*models.SkillResponse, error) {
		return skillV1, nil
	}
	fake.GetSkillByNameAndVersionFn = func(_ context.Context, _, _ string) (*models.SkillResponse, error) {
		return skillV1, nil
	}
	fake.CreateSkillFn = func(_ context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
		return &models.SkillResponse{Skill: *req}, nil
	}
	fake.ListAgentsFn = func(_ context.Context, _ *database.AgentFilter, _ string, _ int) ([]*models.AgentResponse, string, error) {
		return []*models.AgentResponse{agentV1}, "", nil
	}
	fake.GetAgentByNameFn = func(_ context.Context, _ string) (*models.AgentResponse, error) {
		return agentV1, nil
	}
	fake.GetAgentByNameAndVersionFn = func(_ context.Context, _, _ string) (*models.AgentResponse, error) {
		return agentV1, nil
	}
	fake.CreateAgentFn = func(_ context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
		return &models.AgentResponse{Agent: *req}, nil
	}
	fake.DeleteAgentFn = func(_ context.Context, _, _ string) error {
		deletedAgent = true
		return nil
	}
	fake.GetDeploymentsFn = func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
		return []*models.Deployment{}, nil
	}

	client, cleanup := newClientWithInProcessServer(t, fake)
	defer cleanup()

	servers, err := client.GetPublishedServers()
	if err != nil {
		t.Fatalf("GetPublishedServers() failed: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("GetPublishedServers() returned unexpected count: got %d, want 2", len(servers))
	}

	serverLatest, err := client.GetServerByName("acme/weather")
	if err != nil {
		t.Fatalf("GetServerByName() failed: %v", err)
	}
	if serverLatest == nil || serverLatest.Server.Version != "2.0.0" {
		t.Fatalf("GetServerByName() returned unexpected server: %#v", serverLatest)
	}

	serverByVersion, err := client.GetServerByNameAndVersion("acme/weather", "1.0.0")
	if err != nil {
		t.Fatalf("GetServerByNameAndVersion() failed: %v", err)
	}
	if serverByVersion == nil || serverByVersion.Server.Version != "1.0.0" {
		t.Fatalf("GetServerByNameAndVersion() returned unexpected server: %#v", serverByVersion)
	}

	serverVersions, err := client.GetServerVersions("acme/weather")
	if err != nil {
		t.Fatalf("GetServerVersions() failed: %v", err)
	}
	if len(serverVersions) != 2 {
		t.Fatalf("GetServerVersions() returned unexpected count: got %d, want 2", len(serverVersions))
	}

	createdServer, err := client.CreateMCPServer(&apiv0.ServerJSON{
		Schema:      model.CurrentSchemaURL,
		Name:        "acme/new-server",
		Description: "New MCP server",
		Version:     "0.1.0",
	})
	if err != nil {
		t.Fatalf("CreateMCPServer() failed: %v", err)
	}
	if createdServer == nil || createdServer.Server.Name != "acme/new-server" {
		t.Fatalf("CreateMCPServer() returned unexpected payload: %#v", createdServer)
	}

	if err := client.DeleteMCPServer("acme/weather", "1.0.0"); err != nil {
		t.Fatalf("DeleteMCPServer() failed: %v", err)
	}
	if !deletedServer {
		t.Fatal("DeleteMCPServer() did not reach registry.DeleteServer")
	}

	skills, err := client.GetSkills()
	if err != nil {
		t.Fatalf("GetSkills() failed: %v", err)
	}
	if len(skills) != 1 || skills[0].Skill.Name != "acme/translate" {
		t.Fatalf("GetSkills() returned unexpected payload: %#v", skills)
	}

	skillByName, err := client.GetSkillByName("acme/translate")
	if err != nil {
		t.Fatalf("GetSkillByName() failed: %v", err)
	}
	if skillByName == nil || skillByName.Skill.Version != "1.0.0" {
		t.Fatalf("GetSkillByName() returned unexpected payload: %#v", skillByName)
	}

	skillByVersion, err := client.GetSkillByNameAndVersion("acme/translate", "1.0.0")
	if err != nil {
		t.Fatalf("GetSkillByNameAndVersion() failed: %v", err)
	}
	if skillByVersion == nil || skillByVersion.Skill.Name != "acme/translate" {
		t.Fatalf("GetSkillByNameAndVersion() returned unexpected payload: %#v", skillByVersion)
	}

	createdSkill, err := client.CreateSkill(&models.SkillJSON{
		Name:        "acme/new-skill",
		Description: "New skill",
		Version:     "0.1.0",
	})
	if err != nil {
		t.Fatalf("CreateSkill() failed: %v", err)
	}
	if createdSkill == nil || createdSkill.Skill.Name != "acme/new-skill" {
		t.Fatalf("CreateSkill() returned unexpected payload: %#v", createdSkill)
	}

	agents, err := client.GetAgents()
	if err != nil {
		t.Fatalf("GetAgents() failed: %v", err)
	}
	if len(agents) != 1 || agents[0].Agent.Name != "acme/planner" {
		t.Fatalf("GetAgents() returned unexpected payload: %#v", agents)
	}

	agentByName, err := client.GetAgentByName("acme/planner")
	if err != nil {
		t.Fatalf("GetAgentByName() failed: %v", err)
	}
	if agentByName == nil || agentByName.Agent.Version != "1.0.0" {
		t.Fatalf("GetAgentByName() returned unexpected payload: %#v", agentByName)
	}

	agentByVersion, err := client.GetAgentByNameAndVersion("acme/planner", "1.0.0")
	if err != nil {
		t.Fatalf("GetAgentByNameAndVersion() failed: %v", err)
	}
	if agentByVersion == nil || agentByVersion.Agent.Name != "acme/planner" {
		t.Fatalf("GetAgentByNameAndVersion() returned unexpected payload: %#v", agentByVersion)
	}

	createdAgent, err := client.CreateAgent(&models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:        "acme/new-agent",
			Description: "New agent",
			Version:     "0.1.0",
		},
		Version: "0.1.0",
	})
	if err != nil {
		t.Fatalf("CreateAgent() failed: %v", err)
	}
	if createdAgent == nil || createdAgent.Agent.Name != "acme/new-agent" {
		t.Fatalf("CreateAgent() returned unexpected payload: %#v", createdAgent)
	}

	if err := client.DeleteAgent("acme/planner", "1.0.0"); err != nil {
		t.Fatalf("DeleteAgent() failed: %v", err)
	}
	if !deletedAgent {
		t.Fatal("DeleteAgent() did not reach registry.DeleteAgent")
	}
}

func TestClientIntegration_DeploymentRoutes_HappyPath(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	fake := newFakeClientRegistry()

	fake.GetDeploymentsFn = func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
		return []*models.Deployment{
			{
				ID:           "dep-list-1",
				ServerName:   "acme/weather",
				Version:      "1.0.0",
				ResourceType: "mcp",
				Status:       "deployed",
				Origin:       "managed",
				PreferRemote: true,
				DeployedAt:   now,
				UpdatedAt:    now,
			},
		}, nil
	}

	var createdDeployments []*models.Deployment
	createdByID := map[string]*models.Deployment{}
	var removedIDs []string
	fake.CreateDeploymentFn = func(_ context.Context, req *models.Deployment) (*models.Deployment, error) {
		createdDeployments = append(createdDeployments, req)
		id := req.ID
		if id == "" {
			id = "dep-created-" + strconv.Itoa(len(createdDeployments))
		}
		created := &models.Deployment{
			ID:           id,
			ServerName:   req.ServerName,
			Version:      req.Version,
			ProviderID:   req.ProviderID,
			ResourceType: req.ResourceType,
			Status:       "deployed",
			Origin:       "managed",
			PreferRemote: req.PreferRemote,
			DeployedAt:   now,
			UpdatedAt:    now,
		}
		createdByID[created.ID] = created
		return created, nil
	}
	fake.GetDeploymentByIDFn = func(_ context.Context, id string) (*models.Deployment, error) {
		deployment, ok := createdByID[id]
		if !ok {
			return nil, database.ErrNotFound
		}
		return deployment, nil
	}
	fake.RemoveDeploymentByIDFn = func(_ context.Context, id string) error {
		if _, ok := createdByID[id]; !ok {
			return database.ErrNotFound
		}
		removedIDs = append(removedIDs, id)
		delete(createdByID, id)
		return nil
	}
	fake.UndeployDeploymentFn = func(_ context.Context, deployment *models.Deployment) error {
		if deployment == nil {
			return database.ErrNotFound
		}
		if _, ok := createdByID[deployment.ID]; !ok {
			return database.ErrNotFound
		}
		removedIDs = append(removedIDs, deployment.ID)
		delete(createdByID, deployment.ID)
		return nil
	}

	client, cleanup := newClientWithInProcessServer(t, fake)
	defer cleanup()

	list, err := client.GetDeployedServers()
	if err != nil {
		t.Fatalf("GetDeployedServers() failed: %v", err)
	}
	if len(list) != 1 || list[0].ServerName != "acme/weather" {
		t.Fatalf("GetDeployedServers() returned unexpected payload: %#v", list)
	}

	deployedServer, err := client.DeployServer(
		"acme/weather",
		"1.0.0",
		map[string]string{"API_KEY": "secret"},
		true,
		"",
	)
	if err != nil {
		t.Fatalf("DeployServer() failed: %v", err)
	}
	if deployedServer == nil || deployedServer.ResourceType != "mcp" {
		t.Fatalf("DeployServer() returned unexpected payload: %#v", deployedServer)
	}
	if deployedServer.ID == "" {
		t.Fatalf("DeployServer() returned empty deployment id: %#v", deployedServer)
	}
	deployedServerSecond, err := client.DeployServer(
		"acme/weather",
		"1.0.0",
		map[string]string{"API_KEY": "secret"},
		true,
		defaultDeployProviderID,
	)
	if err != nil {
		t.Fatalf("second DeployServer() failed: %v", err)
	}
	if deployedServerSecond == nil || deployedServerSecond.ID == "" {
		t.Fatalf("second DeployServer() returned empty deployment id: %#v", deployedServerSecond)
	}
	if deployedServerSecond.ID == deployedServer.ID {
		t.Fatalf("expected distinct deployment IDs, got %q", deployedServer.ID)
	}
	createdByGet, err := client.GetDeploymentByID(deployedServer.ID)
	if err != nil {
		t.Fatalf("GetDeploymentByID() failed: %v", err)
	}
	if createdByGet == nil || createdByGet.ID != deployedServer.ID {
		t.Fatalf("GetDeploymentByID() returned unexpected payload: %#v", createdByGet)
	}
	createdByGetSecond, err := client.GetDeploymentByID(deployedServerSecond.ID)
	if err != nil {
		t.Fatalf("GetDeploymentByID(second) failed: %v", err)
	}
	if createdByGetSecond == nil || createdByGetSecond.ID != deployedServerSecond.ID {
		t.Fatalf("GetDeploymentByID(second) returned unexpected payload: %#v", createdByGetSecond)
	}
	if err := client.RemoveDeploymentByID(deployedServer.ID); err != nil {
		t.Fatalf("RemoveDeploymentByID() failed: %v", err)
	}
	if err := client.RemoveDeploymentByID(deployedServerSecond.ID); err != nil {
		t.Fatalf("RemoveDeploymentByID(second) failed: %v", err)
	}

	deployedAgent, err := client.DeployAgent(
		"acme/planner",
		"2.0.0",
		map[string]string{"MODE": "fast"},
		"",
	)
	if err != nil {
		t.Fatalf("DeployAgent() failed: %v", err)
	}
	if deployedAgent == nil || deployedAgent.ResourceType != "agent" {
		t.Fatalf("DeployAgent() returned unexpected payload: %#v", deployedAgent)
	}
	if deployedAgent.ID == "" {
		t.Fatalf("DeployAgent() returned empty deployment id: %#v", deployedAgent)
	}

	// Regression: redeploying the same agent/version should produce a new deployment ID.
	deployedAgentSecond, err := client.DeployAgent(
		"acme/planner",
		"2.0.0",
		map[string]string{"MODE": "fast"},
		defaultDeployProviderID,
	)
	if err != nil {
		t.Fatalf("second DeployAgent() failed: %v", err)
	}
	if deployedAgentSecond == nil || deployedAgentSecond.ResourceType != "agent" {
		t.Fatalf("second DeployAgent() returned unexpected payload: %#v", deployedAgentSecond)
	}
	if deployedAgentSecond.ID == "" {
		t.Fatalf("second DeployAgent() returned empty deployment id: %#v", deployedAgentSecond)
	}
	if deployedAgentSecond.ID == deployedAgent.ID {
		t.Fatalf("expected distinct agent deployment IDs, got %q", deployedAgent.ID)
	}

	if err := client.RemoveDeploymentByID(deployedAgent.ID); err != nil {
		t.Fatalf("RemoveDeploymentByID(agent) failed: %v", err)
	}
	if err := client.RemoveDeploymentByID(deployedAgentSecond.ID); err != nil {
		t.Fatalf("RemoveDeploymentByID(agent second) failed: %v", err)
	}

	if len(createdDeployments) != 4 {
		t.Fatalf("expected 4 CreateDeployment() calls, got %d", len(createdDeployments))
	}
	if createdDeployments[0].ResourceType != "mcp" ||
		createdDeployments[1].ResourceType != "mcp" ||
		createdDeployments[2].ResourceType != "agent" ||
		createdDeployments[3].ResourceType != "agent" {
		t.Fatalf("unexpected deployment resource types: %#v", createdDeployments)
	}
	if createdDeployments[0].ProviderID != "local" ||
		createdDeployments[1].ProviderID != "local" ||
		createdDeployments[2].ProviderID != "local" ||
		createdDeployments[3].ProviderID != "local" {
		t.Fatalf("unexpected deployment provider IDs: %#v", createdDeployments)
	}
	if len(removedIDs) != 4 ||
		removedIDs[0] != deployedServer.ID ||
		removedIDs[1] != deployedServerSecond.ID ||
		removedIDs[2] != deployedAgent.ID ||
		removedIDs[3] != deployedAgentSecond.ID {
		t.Fatalf(
			"expected removal of deployments %q, %q, %q, %q; got %#v",
			deployedServer.ID,
			deployedServerSecond.ID,
			deployedAgent.ID,
			deployedAgentSecond.ID,
			removedIDs,
		)
	}
}

func newClientWithInProcessServer(t *testing.T, fake *fakeClientRegistry) (*Client, func()) {
	t.Helper()

	mux := http.NewServeMux()
	meter := noop.NewMeterProvider().Meter("client-integration-tests")
	metrics, err := telemetry.NewMetrics(meter)
	if err != nil {
		t.Fatalf("failed to initialize test metrics: %v", err)
	}

	versionInfo := &apitypes.VersionBody{
		Version:   "test-version",
		GitCommit: "test-commit",
		BuildTime: "2026-01-02T03:04:05Z",
	}
	cfg := &config.Config{
		// Auth endpoints are registered as part of the real router; provide a valid
		// deterministic Ed25519 seed to avoid init panics in JWT manager setup.
		JWTPrivateKey: "0000000000000000000000000000000000000000000000000000000000000000",
	}

	routeOpts := &router.RouteOptions{
		ProviderPlatforms: map[string]registrytypes.ProviderPlatformAdapter{
			"local": &testProviderAdapter{
				provider: &models.Provider{
					ID:       defaultDeployProviderID,
					Name:     "Local provider",
					Platform: "local",
				},
			},
		},
	}

	store := newFakeClientStore(fake)
	deploymentAdapter := &fakeClientDeploymentAdapter{registry: fake}

	router.NewHumaAPI(
		cfg,
		serversvc.New(serversvc.Dependencies{StoreDB: store, Config: cfg}),
		agentsvc.New(agentsvc.Dependencies{StoreDB: store, Config: cfg}),
		skillsvc.New(skillsvc.Dependencies{StoreDB: store}),
		promptsvc.New(promptsvc.Dependencies{StoreDB: store}),
		store,
		deploymentsvc.New(deploymentsvc.Dependencies{
			StoreDB:            store,
			DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{"local": deploymentAdapter},
		}),
		mux,
		metrics,
		versionInfo,
		nil,
		nil,
		routeOpts,
	)
	server := httptest.NewServer(mux)

	client := NewClient(server.URL+"/v0", "test-token")
	return client, server.Close
}

type testProviderAdapter struct {
	provider *models.Provider
}

func (a *testProviderAdapter) Platform() string {
	return "local"
}

func (a *testProviderAdapter) ListProviders(_ context.Context) ([]*models.Provider, error) {
	if a.provider == nil {
		return []*models.Provider{}, nil
	}
	return []*models.Provider{a.provider}, nil
}

func (a *testProviderAdapter) CreateProvider(_ context.Context, _ *models.CreateProviderInput) (*models.Provider, error) {
	return nil, errors.New("not implemented in test provider adapter")
}

func (a *testProviderAdapter) GetProvider(_ context.Context, providerID string) (*models.Provider, error) {
	if a.provider != nil && a.provider.ID == providerID {
		return a.provider, nil
	}
	return nil, database.ErrNotFound
}

func (a *testProviderAdapter) UpdateProvider(_ context.Context, _ string, _ *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, errors.New("not implemented in test provider adapter")
}

func (a *testProviderAdapter) DeleteProvider(_ context.Context, _ string) error {
	return errors.New("not implemented in test provider adapter")
}

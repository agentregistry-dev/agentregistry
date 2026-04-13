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
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
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

// Compile-time check: fakeClientRegistry must implement database.Store.
var _ database.Store = (*fakeClientRegistry)(nil)

type fakeClientRegistry struct {
	ServerList []*apiv0.ServerResponse
	AgentList  []*models.AgentResponse
	SkillList  []*models.SkillResponse
	PromptList []*models.PromptResponse

	deploymentStore *fakeDeploymentStore

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
	GetDeploymentLogsFn    func(ctx context.Context, deployment *models.Deployment) ([]string, error)
	CancelDeploymentFn     func(ctx context.Context, deployment *models.Deployment) error
}

func newFakeClientRegistry() *fakeClientRegistry {
	f := &fakeClientRegistry{}
	f.deploymentStore = newFakeDeploymentStore(f)
	return f
}

// database.Store implementation — fakeClientRegistry acts as all store types.
func (f *fakeClientRegistry) Servers() database.ServerStore     { return f }
func (f *fakeClientRegistry) Agents() database.AgentStore       { return f }
func (f *fakeClientRegistry) Skills() database.SkillStore       { return f }
func (f *fakeClientRegistry) Prompts() database.PromptStore     { return f }
func (f *fakeClientRegistry) Providers() database.ProviderStore { return f }
func (f *fakeClientRegistry) Deployments() database.DeploymentStore {
	return f.deploymentStore
}
func (f *fakeClientRegistry) InTransaction(ctx context.Context, fn func(context.Context, database.Scope) error) error {
	return fn(ctx, f)
}
func (f *fakeClientRegistry) Close() error { return nil }

func (f *fakeClientRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.ListServersFn != nil {
		return f.ListServersFn(ctx, filter, cursor, limit)
	}
	if cursor != "" {
		return nil, "", nil
	}
	return f.ServerList, "", nil
}
func (f *fakeClientRegistry) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameFn != nil {
		return f.GetServerByNameFn(ctx, serverName)
	}
	if len(f.ServerList) > 0 {
		return f.ServerList[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameAndVersionFn != nil {
		return f.GetServerByNameAndVersionFn(ctx, serverName, version)
	}
	return f.GetServer(ctx, serverName)
}
func (f *fakeClientRegistry) GetServerVersions(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if f.GetAllVersionsByServerNameFn != nil {
		return f.GetAllVersionsByServerNameFn(ctx, serverName)
	}
	return f.ServerList, nil
}

// CreateServer implements database.ServerStore — ignores extensions, delegates to CreateServerFn.
func (f *fakeClientRegistry) CreateServer(ctx context.Context, req *apiv0.ServerJSON, _ *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	if f.CreateServerFn != nil {
		return f.CreateServerFn(ctx, req)
	}
	return nil, database.ErrNotFound
}

// UpdateServer implements database.ServerStore — ignores newStatus, delegates to UpdateServerFn.
func (f *fakeClientRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if f.UpdateServerFn != nil {
		return f.UpdateServerFn(ctx, serverName, version, req, nil)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) SetServerStatus(_ context.Context, _, _, _ string) (*apiv0.ServerResponse, error) {
	return nil, nil
}
func (f *fakeClientRegistry) GetLatestServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return f.GetServer(ctx, serverName)
}
func (f *fakeClientRegistry) CountServerVersions(_ context.Context, serverName string) (int, error) {
	count := 0
	for _, s := range f.ServerList {
		if s.Server.Name == serverName {
			count++
		}
	}
	return count, nil
}
func (f *fakeClientRegistry) CheckVersionExists(_ context.Context, serverName, version string) (bool, error) {
	for _, s := range f.ServerList {
		if s.Server.Name == serverName && s.Server.Version == version {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeClientRegistry) UnmarkAsLatest(_ context.Context, _ string) error { return nil }
func (f *fakeClientRegistry) AcquireServerCreateLock(_ context.Context, _ string) error {
	return nil
}
func (f *fakeClientRegistry) SetServerEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	return nil
}
func (f *fakeClientRegistry) GetServerEmbeddingMetadata(_ context.Context, _, _ string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UpsertServerReadme(ctx context.Context, readme *database.ServerReadme) error {
	if readme == nil {
		return nil
	}
	if f.StoreServerReadmeFn != nil {
		return f.StoreServerReadmeFn(ctx, readme.ServerName, readme.Version, readme.Content, readme.ContentType)
	}
	return nil
}
func (f *fakeClientRegistry) GetServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if f.GetServerReadmeByVersionFn != nil {
		return f.GetServerReadmeByVersionFn(ctx, serverName, version)
	}
	return f.GetLatestServerReadme(ctx, serverName)
}
func (f *fakeClientRegistry) GetLatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if f.GetServerReadmeLatestFn != nil {
		return f.GetServerReadmeLatestFn(ctx, serverName)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) DeleteServer(ctx context.Context, serverName, version string) error {
	if f.DeleteServerFn != nil {
		return f.DeleteServerFn(ctx, serverName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.ListAgentsFn != nil {
		return f.ListAgentsFn(ctx, filter, cursor, limit)
	}
	if cursor != "" {
		return nil, "", nil
	}
	return f.AgentList, "", nil
}
func (f *fakeClientRegistry) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.GetAgentByNameFn != nil {
		return f.GetAgentByNameFn(ctx, agentName)
	}
	if len(f.AgentList) > 0 {
		return f.AgentList[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.GetAgentByNameAndVersionFn != nil {
		return f.GetAgentByNameAndVersionFn(ctx, agentName, version)
	}
	return f.GetAgent(ctx, agentName)
}
func (f *fakeClientRegistry) GetAgentVersions(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if f.GetAllVersionsByAgentNameFn != nil {
		return f.GetAllVersionsByAgentNameFn(ctx, agentName)
	}
	return f.AgentList, nil
}

// CreateAgent implements database.AgentStore — ignores extensions, delegates to CreateAgentFn.
func (f *fakeClientRegistry) CreateAgent(ctx context.Context, req *models.AgentJSON, _ *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	if f.CreateAgentFn != nil {
		return f.CreateAgentFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UpdateAgent(_ context.Context, _, _ string, _ *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}
func (f *fakeClientRegistry) SetAgentStatus(_ context.Context, _, _, _ string) (*models.AgentResponse, error) {
	return nil, nil
}
func (f *fakeClientRegistry) GetLatestAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return f.GetAgent(ctx, agentName)
}
func (f *fakeClientRegistry) CountAgentVersions(_ context.Context, agentName string) (int, error) {
	count := 0
	for _, a := range f.AgentList {
		if a.Agent.Name == agentName {
			count++
		}
	}
	return count, nil
}
func (f *fakeClientRegistry) CheckAgentVersionExists(_ context.Context, agentName, version string) (bool, error) {
	for _, a := range f.AgentList {
		if a.Agent.Name == agentName && a.Agent.Version == version {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeClientRegistry) UnmarkAgentAsLatest(_ context.Context, _ string) error { return nil }
func (f *fakeClientRegistry) DeleteAgent(ctx context.Context, agentName, version string) error {
	if f.DeleteAgentFn != nil {
		return f.DeleteAgentFn(ctx, agentName, version)
	}
	return database.ErrNotFound
}
func (f *fakeClientRegistry) SetAgentEmbedding(_ context.Context, _, _ string, _ *database.SemanticEmbedding) error {
	return nil
}
func (f *fakeClientRegistry) GetAgentEmbeddingMetadata(_ context.Context, _, _ string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if f.ListSkillsFn != nil {
		return f.ListSkillsFn(ctx, filter, cursor, limit)
	}
	return f.SkillList, "", nil
}
func (f *fakeClientRegistry) GetSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if f.GetSkillByNameFn != nil {
		return f.GetSkillByNameFn(ctx, skillName)
	}
	if len(f.SkillList) > 0 {
		return f.SkillList[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetSkillVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if f.GetSkillByNameAndVersionFn != nil {
		return f.GetSkillByNameAndVersionFn(ctx, skillName, version)
	}
	return f.GetSkill(ctx, skillName)
}
func (f *fakeClientRegistry) GetSkillVersions(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if f.GetAllVersionsBySkillNameFn != nil {
		return f.GetAllVersionsBySkillNameFn(ctx, skillName)
	}
	return f.SkillList, nil
}

// CreateSkill implements database.SkillStore — ignores extensions, delegates to CreateSkillFn.
func (f *fakeClientRegistry) CreateSkill(ctx context.Context, req *models.SkillJSON, _ *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	if f.CreateSkillFn != nil {
		return f.CreateSkillFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) UpdateSkill(_ context.Context, _, _ string, _ *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}
func (f *fakeClientRegistry) SetSkillStatus(_ context.Context, _, _, _ string) (*models.SkillResponse, error) {
	return nil, nil
}
func (f *fakeClientRegistry) GetLatestSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	return f.GetSkill(ctx, skillName)
}
func (f *fakeClientRegistry) CountSkillVersions(_ context.Context, skillName string) (int, error) {
	count := 0
	for _, s := range f.SkillList {
		if s.Skill.Name == skillName {
			count++
		}
	}
	return count, nil
}
func (f *fakeClientRegistry) CheckSkillVersionExists(_ context.Context, skillName, version string) (bool, error) {
	for _, s := range f.SkillList {
		if s.Skill.Name == skillName && s.Skill.Version == version {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeClientRegistry) UnmarkSkillAsLatest(_ context.Context, _ string) error { return nil }
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
	return f.PromptList, "", nil
}
func (f *fakeClientRegistry) GetPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	if f.GetPromptByNameFn != nil {
		return f.GetPromptByNameFn(ctx, promptName)
	}
	if len(f.PromptList) > 0 {
		return f.PromptList[0], nil
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetPromptVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	if f.GetPromptByNameAndVersionFn != nil {
		return f.GetPromptByNameAndVersionFn(ctx, promptName, version)
	}
	return f.GetPrompt(ctx, promptName)
}
func (f *fakeClientRegistry) GetPromptVersions(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	if f.GetAllVersionsByPromptNameFn != nil {
		return f.GetAllVersionsByPromptNameFn(ctx, promptName)
	}
	return f.PromptList, nil
}

// CreatePrompt implements database.PromptStore — ignores extensions, delegates to CreatePromptFn.
func (f *fakeClientRegistry) CreatePrompt(ctx context.Context, req *models.PromptJSON, _ *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	if f.CreatePromptFn != nil {
		return f.CreatePromptFn(ctx, req)
	}
	return nil, database.ErrNotFound
}
func (f *fakeClientRegistry) GetLatestPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	return f.GetPrompt(ctx, promptName)
}
func (f *fakeClientRegistry) CountPromptVersions(_ context.Context, promptName string) (int, error) {
	count := 0
	for _, p := range f.PromptList {
		if p.Prompt.Name == promptName {
			count++
		}
	}
	return count, nil
}
func (f *fakeClientRegistry) CheckPromptVersionExists(_ context.Context, promptName, version string) (bool, error) {
	for _, p := range f.PromptList {
		if p.Prompt.Name == promptName && p.Prompt.Version == version {
			return true, nil
		}
	}
	return false, nil
}
func (f *fakeClientRegistry) UnmarkPromptAsLatest(_ context.Context, _ string) error { return nil }
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
func (f *fakeClientRegistry) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.GetProviderByIDFn != nil {
		return f.GetProviderByIDFn(ctx, providerID)
	}
	return &models.Provider{ID: providerID, Name: "Local provider", Platform: "local"}, nil
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
func (f *fakeClientRegistry) ReconcileAll(_ context.Context) error { return nil }

type fakeDeploymentStore struct {
	registry         *fakeClientRegistry
	deployments      map[string]*models.Deployment
	nextDeploymentID int
}

func newFakeDeploymentStore(registry *fakeClientRegistry) *fakeDeploymentStore {
	return &fakeDeploymentStore{registry: registry, deployments: map[string]*models.Deployment{}, nextDeploymentID: 1}
}

func (s *fakeDeploymentStore) CreateDeployment(ctx context.Context, req *models.Deployment) error {
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

func (s *fakeDeploymentStore) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if s.registry.GetDeploymentsFn != nil {
		return s.registry.GetDeploymentsFn(ctx, filter)
	}
	deployments := make([]*models.Deployment, 0, len(s.deployments))
	for _, deployment := range s.deployments {
		deployments = append(deployments, deployment)
	}
	return deployments, nil
}

func (s *fakeDeploymentStore) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	if s.registry.GetDeploymentByIDFn != nil {
		return s.registry.GetDeploymentByIDFn(ctx, id)
	}
	if deployment, ok := s.deployments[id]; ok {
		return deployment, nil
	}
	return nil, database.ErrNotFound
}

func (s *fakeDeploymentStore) UpdateDeploymentState(_ context.Context, id string, patch *models.DeploymentStatePatch) error {
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

func (s *fakeDeploymentStore) DeleteDeployment(ctx context.Context, id string) error {
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
	fn := a.registry.DeployServerFn
	if deployment.ResourceType == "agent" {
		fn = a.registry.DeployAgentFn
	}
	if fn != nil {
		result, err := fn(ctx, deployment.ServerName, deployment.Version, deployment.Env, deployment.PreferRemote, deployment.ProviderID)
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

	serverLatest, err := client.GetServer("acme/weather")
	if err != nil {
		t.Fatalf("GetServer() failed: %v", err)
	}
	if serverLatest == nil || serverLatest.Server.Version != "2.0.0" {
		t.Fatalf("GetServer() returned unexpected server: %#v", serverLatest)
	}

	serverByVersion, err := client.GetServerVersion("acme/weather", "1.0.0")
	if err != nil {
		t.Fatalf("GetServerVersion() failed: %v", err)
	}
	if serverByVersion == nil || serverByVersion.Server.Version != "1.0.0" {
		t.Fatalf("GetServerVersion() returned unexpected server: %#v", serverByVersion)
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

	skillByName, err := client.GetSkill("acme/translate")
	if err != nil {
		t.Fatalf("GetSkill() failed: %v", err)
	}
	if skillByName == nil || skillByName.Skill.Version != "1.0.0" {
		t.Fatalf("GetSkill() returned unexpected payload: %#v", skillByName)
	}

	skillByVersion, err := client.GetSkillVersion("acme/translate", "1.0.0")
	if err != nil {
		t.Fatalf("GetSkillVersion() failed: %v", err)
	}
	if skillByVersion == nil || skillByVersion.Skill.Name != "acme/translate" {
		t.Fatalf("GetSkillVersion() returned unexpected payload: %#v", skillByVersion)
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

	agentByName, err := client.GetAgent("acme/planner")
	if err != nil {
		t.Fatalf("GetAgent() failed: %v", err)
	}
	if agentByName == nil || agentByName.Agent.Version != "1.0.0" {
		t.Fatalf("GetAgent() returned unexpected payload: %#v", agentByName)
	}

	agentByVersion, err := client.GetAgentVersion("acme/planner", "1.0.0")
	if err != nil {
		t.Fatalf("GetAgentVersion() failed: %v", err)
	}
	if agentByVersion == nil || agentByVersion.Agent.Name != "acme/planner" {
		t.Fatalf("GetAgentVersion() returned unexpected payload: %#v", agentByVersion)
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
	fake.GetServerByNameAndVersionFn = func(_ context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
		return &apiv0.ServerResponse{
			Server: apiv0.ServerJSON{Name: serverName, Version: version},
		}, nil
	}
	fake.GetAgentByNameAndVersionFn = func(_ context.Context, agentName, version string) (*models.AgentResponse, error) {
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{Name: agentName, Version: version},
				Version:       version,
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
	createdByGet, err := client.GetDeployment(deployedServer.ID)
	if err != nil {
		t.Fatalf("GetDeployment() failed: %v", err)
	}
	if createdByGet == nil || createdByGet.ID != deployedServer.ID {
		t.Fatalf("GetDeployment() returned unexpected payload: %#v", createdByGet)
	}
	createdByGetSecond, err := client.GetDeployment(deployedServerSecond.ID)
	if err != nil {
		t.Fatalf("GetDeployment(second) failed: %v", err)
	}
	if createdByGetSecond == nil || createdByGetSecond.ID != deployedServerSecond.ID {
		t.Fatalf("GetDeployment(second) returned unexpected payload: %#v", createdByGetSecond)
	}
	if err := client.DeleteDeployment(deployedServer.ID); err != nil {
		t.Fatalf("DeleteDeployment() failed: %v", err)
	}
	if err := client.DeleteDeployment(deployedServerSecond.ID); err != nil {
		t.Fatalf("DeleteDeployment(second) failed: %v", err)
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

	if err := client.DeleteDeployment(deployedAgent.ID); err != nil {
		t.Fatalf("DeleteDeployment(agent) failed: %v", err)
	}
	if err := client.DeleteDeployment(deployedAgentSecond.ID); err != nil {
		t.Fatalf("DeleteDeployment(agent second) failed: %v", err)
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

	deploymentAdapter := &fakeClientDeploymentAdapter{registry: fake}
	providerRegistry := providersvc.New(providersvc.Dependencies{
		StoreDB:           fake,
		ProviderPlatforms: routeOpts.ProviderPlatforms,
	})

	router.NewHumaAPI(
		cfg,
		router.RegistryServices{
			Server:   serversvc.New(serversvc.Dependencies{StoreDB: fake, Config: cfg}),
			Agent:    agentsvc.New(agentsvc.Dependencies{StoreDB: fake, Config: cfg}),
			Skill:    skillsvc.New(skillsvc.Dependencies{StoreDB: fake}),
			Prompt:   promptsvc.New(promptsvc.Dependencies{StoreDB: fake}),
			Provider: providerRegistry,
			Deployment: deploymentsvc.New(deploymentsvc.Dependencies{
				StoreDB:            fake,
				Providers:          providerRegistry,
				DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{"local": deploymentAdapter},
			}),
		},
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

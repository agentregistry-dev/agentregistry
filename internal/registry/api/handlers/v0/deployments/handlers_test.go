package deployments_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	v0deployments "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/deployments"
	platformutils "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/utils"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeDeploymentAdapter struct {
	deployCalled   bool
	deployErr      error
	undeployErr    error
	getLogsErr     error
	cancelErr      error
	undeployCalled bool
	getLogsCalled  bool
	cancelCalled   bool
	lastDeployReq  *models.Deployment
}

type fakeProviderDeploymentService struct {
	GetProviderByIDFn           func(ctx context.Context, providerID string) (*models.Provider, error)
	GetServerByNameAndVersionFn func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	GetAgentByNameAndVersionFn  func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	CreateDeploymentRecordFn    func(ctx context.Context, req *models.Deployment) (*models.Deployment, error)
	GetDeploymentByIDFn         func(ctx context.Context, id string) (*models.Deployment, error)
	RemoveDeploymentByIDFn      func(ctx context.Context, id string) error
	UpdateDeploymentStateFn     func(ctx context.Context, id string, patch *models.DeploymentStatePatch) error
	GetDeploymentsFn            func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	deployments                 map[string]*models.Deployment
	nextDeploymentID            int
}

func newFakeProviderDeploymentService() *fakeProviderDeploymentService {
	return &fakeProviderDeploymentService{
		deployments:      map[string]*models.Deployment{},
		nextDeploymentID: 1,
	}
}

func (f *fakeProviderDeploymentService) ListProviders(context.Context, *string) ([]*models.Provider, error) {
	return nil, nil
}

func (f *fakeProviderDeploymentService) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if f.GetProviderByIDFn != nil {
		return f.GetProviderByIDFn(ctx, providerID)
	}
	if strings.TrimSpace(providerID) == "" {
		return nil, database.ErrNotFound
	}
	return &models.Provider{ID: providerID, Platform: providerID}, nil
}

func (f *fakeProviderDeploymentService) CreateProvider(context.Context, *models.CreateProviderInput) (*models.Provider, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) UpdateProvider(context.Context, string, *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) DeleteProvider(context.Context, string) error {
	return database.ErrNotFound
}

func (f *fakeProviderDeploymentService) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.GetDeploymentsFn != nil {
		return f.GetDeploymentsFn(ctx, filter)
	}
	deployments := make([]*models.Deployment, 0, len(f.deployments))
	for _, deployment := range f.deployments {
		deployments = append(deployments, deployment)
	}
	return deployments, nil
}

func (f *fakeProviderDeploymentService) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	if f.GetDeploymentByIDFn != nil {
		return f.GetDeploymentByIDFn(ctx, id)
	}
	if deployment, ok := f.deployments[id]; ok {
		return deployment, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) CreateDeployment(ctx context.Context, req *models.Deployment) error {
	created := req
	if f.CreateDeploymentRecordFn != nil {
		var err error
		created, err = f.CreateDeploymentRecordFn(ctx, req)
		if err != nil {
			return err
		}
	}
	if created == nil {
		created = req
	}
	stored := *created
	if strings.TrimSpace(stored.ID) == "" {
		stored.ID = fmt.Sprintf("dep-%d", f.nextDeploymentID)
		f.nextDeploymentID++
	}
	req.ID = stored.ID
	if stored.Env == nil {
		stored.Env = map[string]string{}
	}
	f.deployments[stored.ID] = &stored
	return nil
}

func (f *fakeProviderDeploymentService) UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
	if deployment, ok := f.deployments[id]; ok {
		if patch.Status != nil {
			deployment.Status = *patch.Status
		}
		if patch.Error != nil {
			deployment.Error = *patch.Error
		}
		if patch.ProviderConfig != nil {
			deployment.ProviderConfig = *patch.ProviderConfig
		}
		if patch.ProviderMetadata != nil {
			deployment.ProviderMetadata = *patch.ProviderMetadata
		}
	}
	if f.UpdateDeploymentStateFn != nil {
		return f.UpdateDeploymentStateFn(ctx, id, patch)
	}
	if _, ok := f.deployments[id]; !ok {
		return database.ErrNotFound
	}
	return nil
}

func (f *fakeProviderDeploymentService) DeleteDeployment(ctx context.Context, id string) error {
	if f.RemoveDeploymentByIDFn != nil {
		return f.RemoveDeploymentByIDFn(ctx, id)
	}
	delete(f.deployments, id)
	return nil
}

func (f *fakeProviderDeploymentService) DeleteServer(context.Context, string, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) CreateServer(context.Context, *apiv0.ServerJSON, *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) UpdateServer(context.Context, string, string, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) SetServerStatus(context.Context, string, string, string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", nil
}

func (f *fakeProviderDeploymentService) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return f.GetServerVersion(ctx, serverName, "latest")
}

func (f *fakeProviderDeploymentService) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.GetServerByNameAndVersionFn != nil {
		return f.GetServerByNameAndVersionFn(ctx, serverName, version)
	}
	return &apiv0.ServerResponse{Server: apiv0.ServerJSON{Name: serverName, Version: version}}, nil
}

func (f *fakeProviderDeploymentService) GetServerVersions(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	server, err := f.GetServer(ctx, serverName)
	if err != nil {
		return nil, err
	}
	return []*apiv0.ServerResponse{server}, nil
}

func (f *fakeProviderDeploymentService) GetLatestServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	return f.GetServer(ctx, serverName)
}

func (f *fakeProviderDeploymentService) CountServerVersions(context.Context, string) (int, error) {
	return 1, nil
}

func (f *fakeProviderDeploymentService) CheckVersionExists(context.Context, string, string) (bool, error) {
	return true, nil
}

func (f *fakeProviderDeploymentService) UnmarkAsLatest(context.Context, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) AcquireServerCreateLock(context.Context, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) SetServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakeProviderDeploymentService) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) UpsertServerReadme(context.Context, *database.ServerReadme) error {
	return nil
}

func (f *fakeProviderDeploymentService) GetServerReadme(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetLatestServerReadme(context.Context, string) (*database.ServerReadme, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) CreateAgent(context.Context, *models.AgentJSON, *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) UpdateAgent(context.Context, string, string, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) SetAgentStatus(context.Context, string, string, string) (*models.AgentResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	return nil, "", nil
}

func (f *fakeProviderDeploymentService) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return f.GetAgentVersion(ctx, agentName, "latest")
}

func (f *fakeProviderDeploymentService) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.GetAgentByNameAndVersionFn != nil {
		return f.GetAgentByNameAndVersionFn(ctx, agentName, version)
	}
	return &models.AgentResponse{Agent: models.AgentJSON{AgentManifest: models.AgentManifest{Name: agentName}, Version: version}}, nil
}

func (f *fakeProviderDeploymentService) GetAgentVersions(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	agent, err := f.GetAgent(ctx, agentName)
	if err != nil {
		return nil, err
	}
	return []*models.AgentResponse{agent}, nil
}

func (f *fakeProviderDeploymentService) GetLatestAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	return f.GetAgent(ctx, agentName)
}

func (f *fakeProviderDeploymentService) CountAgentVersions(context.Context, string) (int, error) {
	return 1, nil
}

func (f *fakeProviderDeploymentService) CheckAgentVersionExists(context.Context, string, string) (bool, error) {
	return true, nil
}

func (f *fakeProviderDeploymentService) UnmarkAgentAsLatest(context.Context, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) DeleteAgent(context.Context, string, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) SetAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return nil
}

func (f *fakeProviderDeploymentService) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) CreateSkill(context.Context, *models.SkillJSON, *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) UpdateSkill(context.Context, string, string, *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) SetSkillStatus(context.Context, string, string, string) (*models.SkillResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) ListSkills(context.Context, *database.SkillFilter, string, int) ([]*models.SkillResponse, string, error) {
	return nil, "", nil
}

func (f *fakeProviderDeploymentService) GetSkill(context.Context, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetSkillVersion(context.Context, string, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetSkillVersions(context.Context, string) ([]*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetLatestSkill(context.Context, string) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) CountSkillVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (f *fakeProviderDeploymentService) CheckSkillVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeProviderDeploymentService) UnmarkSkillAsLatest(context.Context, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) DeleteSkill(context.Context, string, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) CreatePrompt(context.Context, *models.PromptJSON, *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	return nil, database.ErrInvalidInput
}

func (f *fakeProviderDeploymentService) ListPrompts(context.Context, *database.PromptFilter, string, int) ([]*models.PromptResponse, string, error) {
	return nil, "", nil
}

func (f *fakeProviderDeploymentService) GetPrompt(context.Context, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetPromptVersion(context.Context, string, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetPromptVersions(context.Context, string) ([]*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) GetLatestPrompt(context.Context, string) (*models.PromptResponse, error) {
	return nil, database.ErrNotFound
}

func (f *fakeProviderDeploymentService) CountPromptVersions(context.Context, string) (int, error) {
	return 0, nil
}

func (f *fakeProviderDeploymentService) CheckPromptVersionExists(context.Context, string, string) (bool, error) {
	return false, nil
}

func (f *fakeProviderDeploymentService) UnmarkPromptAsLatest(context.Context, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) Servers() database.ServerStore { return f }

func (f *fakeProviderDeploymentService) Providers() database.ProviderStore { return f }

func (f *fakeProviderDeploymentService) Agents() database.AgentStore { return f }

func (f *fakeProviderDeploymentService) Skills() database.SkillStore { return f }

func (f *fakeProviderDeploymentService) Prompts() database.PromptStore { return f }

func (f *fakeProviderDeploymentService) Deployments() database.DeploymentStore { return f }

func (f *fakeProviderDeploymentService) DeletePrompt(context.Context, string, string) error {
	return nil
}

func (f *fakeProviderDeploymentService) InTransaction(ctx context.Context, fn func(context.Context, database.Scope) error) error {
	return fn(ctx, f)
}

func (f *fakeProviderDeploymentService) Close() error {
	return nil
}

func newTestDeploymentService(store *fakeProviderDeploymentService, adapters map[string]registrytypes.DeploymentPlatformAdapter) deploymentsvc.Registry {
	return deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:            store,
		DeploymentAdapters: adapters,
	})
}

func registerDeploymentTestEndpoints(mux *http.ServeMux, store *fakeProviderDeploymentService, adapters map[string]registrytypes.DeploymentPlatformAdapter) {
	api := humago.New(mux, huma.DefaultConfig("Test API", "1.0.0"))
	v0deployments.RegisterDeploymentsEndpoints(api, "/v0", newTestDeploymentService(store, adapters))
}

func (f *fakeDeploymentAdapter) Platform() string { return "local" }
func (f *fakeDeploymentAdapter) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}
func (f *fakeDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	f.deployCalled = true
	f.lastDeployReq = req
	if f.deployErr != nil {
		return nil, f.deployErr
	}
	return &models.DeploymentActionResult{Status: "deployed"}, nil
}

func TestCreateDeployment_PassesEnvAndProviderConfigSeparately(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
		"env": map[string]string{
			"API_KEY": "abc",
		},
		"providerConfig": map[string]any{
			"securityGroupId": "sg-123",
		},
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	require.True(t, adapter.deployCalled)
	require.NotNil(t, adapter.lastDeployReq)
	assert.Equal(t, "abc", adapter.lastDeployReq.Env["API_KEY"])
	var providerCfg map[string]string
	err = adapter.lastDeployReq.ProviderConfig.UnmarshalInto(&providerCfg)
	require.NoError(t, err)
	assert.Equal(t, "sg-123", providerCfg["securityGroupId"])
}

func TestCreateDeployment_MissingProviderIDReturnsBadRequest(t *testing.T) {
	reg := newFakeProviderDeploymentService()

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": &fakeDeploymentAdapter{}})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnprocessableEntity, w.Code)
	assert.Contains(t, w.Body.String(), "required property providerId")
}

func (f *fakeDeploymentAdapter) Undeploy(_ context.Context, _ *models.Deployment) error {
	f.undeployCalled = true
	return f.undeployErr
}
func (f *fakeDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	f.getLogsCalled = true
	if f.getLogsErr != nil {
		return nil, f.getLogsErr
	}
	return []string{"line-1", "line-2"}, nil
}
func (f *fakeDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error {
	f.cancelCalled = true
	return f.cancelErr
}
func (f *fakeDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return []*models.Deployment{}, nil
}

func TestDeleteDeployment_DiscoveredReturnsConflict(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Origin:     "discovered",
		}, nil
	}
	reg.RemoveDeploymentByIDFn = func(ctx context.Context, id string) error {
		return database.ErrInvalidInput
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	mux := http.NewServeMux()
	adapter := &fakeDeploymentAdapter{undeployErr: database.ErrInvalidInput}
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodDelete, "/v0/deployments/dep-discovered-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "Discovered deployments cannot be deleted directly")
}

func TestCreateDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.True(t, adapter.deployCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	var got models.Deployment
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.NotEmpty(t, got.ID)
	assert.Equal(t, "io.github.user/weather", got.ServerName)
}

func TestCreateDeployment_InvalidInputFromAdapterReturnsBadRequest(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}
	adapter := &fakeDeploymentAdapter{deployErr: database.ErrInvalidInput}

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateDeployment_AllowsMultipleDeploymentsForSameArtifact(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": &fakeDeploymentAdapter{}})

	body := map[string]any{
		"serverName":   "io.github.user/weather",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req1 := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	mux.ServeHTTP(w1, req1)

	req2 := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, req2)

	require.Equal(t, http.StatusOK, w1.Code)
	require.Equal(t, http.StatusOK, w2.Code)

	var first models.Deployment
	var second models.Deployment
	require.NoError(t, json.Unmarshal(w1.Body.Bytes(), &first))
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &second))
	assert.NotEmpty(t, first.ID)
	assert.NotEmpty(t, second.ID)
	assert.NotEqual(t, first.ID, second.ID)
}

func TestCreateDeployment_NotFoundIncludesResourceName(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}
	reg.GetServerByNameAndVersionFn = func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
		return nil, database.ErrNotFound
	}

	adapter := &fakeDeploymentAdapter{}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	body := map[string]any{
		"serverName":   "my-cool-server",
		"version":      "1.0.0",
		"resourceType": "mcp",
		"providerId":   "local",
	}
	payload, err := json.Marshal(body)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments", bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "my-cool-server")
}

func TestDeleteDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodDelete, "/v0/deployments/dep-1", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, adapter.undeployCalled)
}

func TestDeleteDeployment_UnsupportedPlatformReturnsBadRequest(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
			Origin:     "managed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{})

	req := httptest.NewRequest(http.MethodDelete, "/v0/deployments/dep-unsupported", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "Unsupported provider or platform for deployment")
}

func TestGetDeploymentLogs_UsesAdapterWhenRegistered(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodGet, "/v0/deployments/dep-2/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.True(t, adapter.getLogsCalled)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "dep-2", resp["deploymentId"])
	assert.Equal(t, "deployed", resp["status"])
	assert.Equal(t, []any{"line-1", "line-2"}, resp["logs"])
}

func TestGetDeploymentLogs_NotFoundFromAdapterReturnsNotFound(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{getLogsErr: database.ErrNotFound}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodGet, "/v0/deployments/dep-2/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.True(t, adapter.getLogsCalled)
}

func TestGetDeploymentLogs_NotSupportedFromAdapterReturnsNotImplemented(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "deployed",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{getLogsErr: platformutils.ErrDeploymentNotSupported}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodGet, "/v0/deployments/dep-2/logs", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.True(t, adapter.getLogsCalled)
}

func TestCancelDeployment_UsesAdapterWhenRegistered(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "queued",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments/dep-3/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.True(t, adapter.cancelCalled)
}

func TestCancelDeployment_InvalidInputFromAdapterReturnsBadRequest(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "queued",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{cancelErr: database.ErrInvalidInput}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments/dep-3/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.True(t, adapter.cancelCalled)
}

func TestCancelDeployment_NotSupportedFromAdapterReturnsNotImplemented(t *testing.T) {
	reg := newFakeProviderDeploymentService()
	reg.GetDeploymentByIDFn = func(ctx context.Context, id string) (*models.Deployment, error) {
		return &models.Deployment{
			ID:         id,
			ProviderID: "local",
			Status:     "queued",
		}, nil
	}
	reg.GetProviderByIDFn = func(ctx context.Context, providerID string) (*models.Provider, error) {
		return &models.Provider{ID: providerID, Platform: "local"}, nil
	}

	adapter := &fakeDeploymentAdapter{cancelErr: platformutils.ErrDeploymentNotSupported}
	mux := http.NewServeMux()
	registerDeploymentTestEndpoints(mux, reg, map[string]registrytypes.DeploymentPlatformAdapter{"local": adapter})

	req := httptest.NewRequest(http.MethodPost, "/v0/deployments/dep-3/cancel", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotImplemented, w.Code)
	assert.True(t, adapter.cancelCalled)
}

package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type fakeMCPRegistry struct {
	servers      []*apiv0.ServerResponse
	agents       []*models.AgentResponse
	skills       []*models.SkillResponse
	deployments  []*models.Deployment
	serverReadme *database.ServerReadme

	listAgentsFn             func(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	getAgentByNameFn         func(ctx context.Context, agentName string) (*models.AgentResponse, error)
	getAgentByNameVersionFn  func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	listServersFn            func(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	getServerByNameVersionFn func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	getAllServerVersionsFn   func(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	getServerReadmeLatestFn  func(ctx context.Context, serverName string) (*database.ServerReadme, error)
	getServerReadmeByVerFn   func(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	listSkillsFn             func(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	getSkillByNameFn         func(ctx context.Context, skillName string) (*models.SkillResponse, error)
	getSkillByNameVersionFn  func(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	getDeploymentsFn         func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	getDeploymentByIDFn      func(ctx context.Context, id string) (*models.Deployment, error)
	createDeploymentRecordFn func(ctx context.Context, deployment *models.Deployment) (*models.Deployment, error)
	deployServerFn           func(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	deployAgentFn            func(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	undeployFn               func(ctx context.Context, deployment *models.Deployment) error
}

func (f *fakeMCPRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.listAgentsFn != nil {
		return f.listAgentsFn(ctx, filter, cursor, limit)
	}
	return f.agents, "", nil
}

func (f *fakeMCPRegistry) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.getAgentByNameFn != nil {
		return f.getAgentByNameFn(ctx, agentName)
	}
	if len(f.agents) > 0 {
		return f.agents[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.getAgentByNameVersionFn != nil {
		return f.getAgentByNameVersionFn(ctx, agentName, version)
	}
	return f.GetAgent(ctx, agentName)
}

func (f *fakeMCPRegistry) GetAgentVersions(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if len(f.agents) > 0 {
		return f.agents, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) PublishAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeleteAgent(ctx context.Context, agentName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return nil, nil
}

func (f *fakeMCPRegistry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return nil, nil
}

func (f *fakeMCPRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.listServersFn != nil {
		return f.listServersFn(ctx, filter, cursor, limit)
	}
	return f.servers, "", nil
}

func (f *fakeMCPRegistry) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if len(f.servers) > 0 {
		return f.servers[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetServerVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.getServerByNameVersionFn != nil {
		return f.getServerByNameVersionFn(ctx, serverName, version)
	}
	if len(f.servers) > 0 {
		return f.servers[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetServerVersions(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if f.getAllServerVersionsFn != nil {
		return f.getAllServerVersionsFn(ctx, serverName)
	}
	return f.servers, nil
}

func (f *fakeMCPRegistry) PublishServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) SetServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetLatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if f.getServerReadmeLatestFn != nil {
		return f.getServerReadmeLatestFn(ctx, serverName)
	}
	if f.serverReadme != nil {
		return f.serverReadme, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if f.getServerReadmeByVerFn != nil {
		return f.getServerReadmeByVerFn(ctx, serverName, version)
	}
	return f.GetLatestServerReadme(ctx, serverName)
}

func (f *fakeMCPRegistry) DeleteServer(ctx context.Context, serverName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if f.listSkillsFn != nil {
		return f.listSkillsFn(ctx, filter, cursor, limit)
	}
	return f.skills, "", nil
}

func (f *fakeMCPRegistry) GetSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if f.getSkillByNameFn != nil {
		return f.getSkillByNameFn(ctx, skillName)
	}
	if len(f.skills) > 0 {
		return f.skills[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetSkillVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if f.getSkillByNameVersionFn != nil {
		return f.getSkillByNameVersionFn(ctx, skillName, version)
	}
	return f.GetSkill(ctx, skillName)
}

func (f *fakeMCPRegistry) GetSkillVersions(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if len(f.skills) > 0 {
		return f.skills, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) PublishSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeleteSkill(ctx context.Context, skillName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.getDeploymentsFn != nil {
		return f.getDeploymentsFn(ctx, filter)
	}
	return f.deployments, nil
}

func (f *fakeMCPRegistry) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	if f.getDeploymentByIDFn != nil {
		return f.getDeploymentByIDFn(ctx, id)
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.deployServerFn != nil {
		return f.deployServerFn(ctx, serverName, version, config, preferRemote, providerID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.deployAgentFn != nil {
		return f.deployAgentFn(ctx, agentName, version, config, preferRemote, providerID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeleteDeployment(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) LaunchDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	if f.undeployFn != nil {
		return f.undeployFn(ctx, deployment)
	}
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	return errors.New("not implemented")
}

type fakeMCPDeploymentHarness struct {
	registry         *fakeMCPRegistry
	deployments      map[string]*models.Deployment
	nextDeploymentID int
}

func newFakeMCPDeploymentHarness(registry *fakeMCPRegistry) *fakeMCPDeploymentHarness {
	return &fakeMCPDeploymentHarness{
		registry:         registry,
		deployments:      map[string]*models.Deployment{},
		nextDeploymentID: 1,
	}
}

func (h *fakeMCPDeploymentHarness) CreateProvider(context.Context, *models.CreateProviderInput) (*models.Provider, error) {
	return nil, errors.New("not implemented")
}

func (h *fakeMCPDeploymentHarness) ListProviders(context.Context, *string) ([]*models.Provider, error) {
	return []*models.Provider{{ID: "local", Platform: "local"}}, nil
}

func (h *fakeMCPDeploymentHarness) GetProvider(context.Context, string) (*models.Provider, error) {
	return &models.Provider{ID: "local", Platform: "local"}, nil
}

func (h *fakeMCPDeploymentHarness) UpdateProvider(context.Context, string, *models.UpdateProviderInput) (*models.Provider, error) {
	return nil, errors.New("not implemented")
}

func (h *fakeMCPDeploymentHarness) DeleteProvider(context.Context, string) error {
	return errors.New("not implemented")
}

func (h *fakeMCPDeploymentHarness) CreateManagedDeploymentRecord(ctx context.Context, deployment *models.Deployment) (*models.Deployment, error) {
	created := deployment
	if h.registry.createDeploymentRecordFn != nil {
		var err error
		created, err = h.registry.createDeploymentRecordFn(ctx, deployment)
		if err != nil {
			return nil, err
		}
	}
	if created == nil {
		created = deployment
	}
	stored := *created
	if stored.ID == "" {
		stored.ID = fmt.Sprintf("dep-%d", h.nextDeploymentID)
		h.nextDeploymentID++
	}
	deployment.ID = stored.ID
	if stored.Env == nil {
		stored.Env = map[string]string{}
	}
	h.deployments[stored.ID] = &stored
	return &stored, nil
}

func (h *fakeMCPDeploymentHarness) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if h.registry.getDeploymentsFn != nil {
		return h.registry.getDeploymentsFn(ctx, filter)
	}
	if len(h.deployments) > 0 {
		deployments := make([]*models.Deployment, 0, len(h.deployments))
		for _, deployment := range h.deployments {
			deployments = append(deployments, deployment)
		}
		return deployments, nil
	}
	return h.registry.deployments, nil
}

func (h *fakeMCPDeploymentHarness) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	if h.registry.getDeploymentByIDFn != nil {
		return h.registry.getDeploymentByIDFn(ctx, id)
	}
	if deployment, ok := h.deployments[id]; ok {
		return deployment, nil
	}
	return nil, database.ErrNotFound
}

func (h *fakeMCPDeploymentHarness) UpdateDeploymentState(_ context.Context, id string, patch *models.DeploymentStatePatch) error {
	deployment, ok := h.deployments[id]
	if !ok {
		return database.ErrNotFound
	}
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
	return nil
}

func (h *fakeMCPDeploymentHarness) DeleteDeployment(_ context.Context, id string) error {
	delete(h.deployments, id)
	return nil
}

func (h *fakeMCPDeploymentHarness) LaunchDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	created, err := h.CreateManagedDeploymentRecord(ctx, req)
	if err != nil {
		return nil, err
	}
	result, deployErr := h.Deploy(ctx, created)
	if deployErr != nil {
		if stateErr := h.ApplyFailedDeploymentAction(ctx, created.ID, deployErr, result); stateErr != nil {
			return nil, stateErr
		}
		return nil, deployErr
	}
	if err := h.ApplyDeploymentActionResult(ctx, created.ID, result); err != nil {
		return nil, err
	}
	return h.GetDeployment(ctx, created.ID)
}

func (h *fakeMCPDeploymentHarness) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return h.LaunchDeployment(ctx, &models.Deployment{
		ServerName:   serverName,
		Version:      version,
		Env:          config,
		PreferRemote: preferRemote,
		ProviderID:   providerID,
		ResourceType: "mcp",
		Origin:       "managed",
	})
}

func (h *fakeMCPDeploymentHarness) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	return h.LaunchDeployment(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      version,
		Env:          config,
		PreferRemote: preferRemote,
		ProviderID:   providerID,
		ResourceType: "agent",
		Origin:       "managed",
	})
}

func (h *fakeMCPDeploymentHarness) ResolveDeploymentAdapter(platform string) (registrytypes.DeploymentPlatformAdapter, error) {
	if platform != "" && platform != "local" {
		return nil, fmt.Errorf("unsupported platform %q", platform)
	}
	return h, nil
}

func (h *fakeMCPDeploymentHarness) ResolveDeploymentAdapterByProviderID(context.Context, string) (registrytypes.DeploymentPlatformAdapter, error) {
	return h, nil
}

func (h *fakeMCPDeploymentHarness) CleanupExistingDeployment(context.Context, string, string, string) error {
	return nil
}

func (h *fakeMCPDeploymentHarness) ApplyDeploymentActionResult(ctx context.Context, deploymentID string, result *models.DeploymentActionResult) error {
	status := models.DeploymentStatusDeployed
	patch := &models.DeploymentStatePatch{}
	if result != nil {
		if result.Status != "" {
			status = result.Status
		}
		if result.ProviderConfig != nil {
			patch.ProviderConfig = &result.ProviderConfig
		}
		if result.ProviderMetadata != nil {
			patch.ProviderMetadata = &result.ProviderMetadata
		}
		if result.Error != "" {
			patch.Error = &result.Error
		}
	}
	patch.Status = &status
	return h.UpdateDeploymentState(ctx, deploymentID, patch)
}

func (h *fakeMCPDeploymentHarness) ApplyFailedDeploymentAction(ctx context.Context, deploymentID string, deployErr error, result *models.DeploymentActionResult) error {
	status := models.DeploymentStatusFailed
	message := ""
	if deployErr != nil {
		message = deployErr.Error()
	}
	patch := &models.DeploymentStatePatch{Status: &status, Error: &message}
	if result != nil {
		if result.ProviderConfig != nil {
			patch.ProviderConfig = &result.ProviderConfig
		}
		if result.ProviderMetadata != nil {
			patch.ProviderMetadata = &result.ProviderMetadata
		}
		if result.Error != "" {
			patch.Error = &result.Error
		}
	}
	return h.UpdateDeploymentState(ctx, deploymentID, patch)
}

func (h *fakeMCPDeploymentHarness) Platform() string { return "local" }

func (h *fakeMCPDeploymentHarness) SupportedResourceTypes() []string {
	return []string{"mcp", "agent"}
}

func (h *fakeMCPDeploymentHarness) Deploy(ctx context.Context, deployment *models.Deployment) (*models.DeploymentActionResult, error) {
	if deployment == nil {
		return nil, database.ErrInvalidInput
	}
	fn := h.registry.deployServerFn
	if deployment.ResourceType == "agent" {
		fn = h.registry.deployAgentFn
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

func (h *fakeMCPDeploymentHarness) Undeploy(ctx context.Context, deployment *models.Deployment) error {
	if h.registry.undeployFn != nil {
		return h.registry.undeployFn(ctx, deployment)
	}
	return nil
}

func (h *fakeMCPDeploymentHarness) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	return h.Undeploy(ctx, deployment)
}

func (h *fakeMCPDeploymentHarness) GetLogs(context.Context, *models.Deployment) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (h *fakeMCPDeploymentHarness) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	return h.GetLogs(ctx, deployment)
}

func (h *fakeMCPDeploymentHarness) Cancel(context.Context, *models.Deployment) error {
	return errors.New("not implemented")
}

func (h *fakeMCPDeploymentHarness) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	return h.Cancel(ctx, deployment)
}

func (h *fakeMCPDeploymentHarness) Discover(context.Context, string) ([]*models.Deployment, error) {
	return nil, nil
}

func newTestMCPServer(reg *fakeMCPRegistry) *mcp.Server {
	deploymentHarness := newFakeMCPDeploymentHarness(reg)
	return NewServer(
		reg,
		reg,
		reg,
		deploymentHarness,
	)
}

func TestServerTools_ListAndReadme(t *testing.T) {
	ctx := context.Background()

	readme := &database.ServerReadme{
		ServerName:  "com.example/echo",
		Version:     "1.0.0",
		Content:     []byte("# Echo"),
		ContentType: "text/markdown",
		SizeBytes:   6,
		SHA256:      []byte{0xaa, 0xbb},
		FetchedAt:   time.Now(),
	}
	reg := &fakeMCPRegistry{servers: []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/echo",
				Description: "Echo server",
				Title:       "Echo",
				Version:     "1.0.0",
			},
		},
	}, serverReadme: readme}

	server := newTestMCPServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	// list_servers
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_servers",
		Arguments: map[string]any{"limit": 10},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var listOut apiv0.ServerListResponse
	require.NoError(t, json.Unmarshal(raw, &listOut))
	require.Len(t, listOut.Servers, 1)
	assert.Equal(t, "com.example/echo", listOut.Servers[0].Server.Name)

	// get_server_readme
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_server_readme",
		Arguments: map[string]any{
			"name": "com.example/echo",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var readmeOut ServerReadmePayload
	require.NoError(t, json.Unmarshal(raw, &readmeOut))
	assert.Equal(t, "com.example/echo", readmeOut.Server)
	assert.Equal(t, "1.0.0", readmeOut.Version)
	assert.Equal(t, "text/markdown", readmeOut.ContentType)
	assert.Equal(t, "aabb", readmeOut.SHA256[:4])

	// get_server (defaults to latest)
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_server",
		Arguments: map[string]any{
			"name": "com.example/echo",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var serverOut apiv0.ServerListResponse
	require.NoError(t, json.Unmarshal(raw, &serverOut))
	require.Len(t, serverOut.Servers, 1)
	assert.Equal(t, "com.example/echo", serverOut.Servers[0].Server.Name)
	assert.Equal(t, "1.0.0", serverOut.Servers[0].Server.Version)
}

func TestAgentAndSkillTools_ListAndGet(t *testing.T) {
	ctx := context.Background()

	reg := &fakeMCPRegistry{agents: []*models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:      "com.example/agent",
					Language:  "go",
					Framework: "none",
				},
				Title:   "Agent",
				Version: "1.0.0",
				Status:  string(model.StatusActive),
			},
		},
	}, skills: []*models.SkillResponse{
		{
			Skill: models.SkillJSON{
				Name:    "com.example/skill",
				Title:   "Skill",
				Version: "2.0.0",
				Status:  string(model.StatusActive),
			},
		},
	}}

	server := newTestMCPServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	// list_agents
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var agentList models.AgentListResponse
	require.NoError(t, json.Unmarshal(raw, &agentList))
	require.Len(t, agentList.Agents, 1)
	assert.Equal(t, "com.example/agent", agentList.Agents[0].Agent.Name)

	// get_agent
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_agent",
		Arguments: map[string]any{
			"name": "com.example/agent",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var agentOne models.AgentResponse
	require.NoError(t, json.Unmarshal(raw, &agentOne))
	assert.Equal(t, "com.example/agent", agentOne.Agent.Name)

	// list_skills
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_skills",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var skillList models.SkillListResponse
	require.NoError(t, json.Unmarshal(raw, &skillList))
	require.Len(t, skillList.Skills, 1)
	assert.Equal(t, "com.example/skill", skillList.Skills[0].Skill.Name)

	// get_skill
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_skill",
		Arguments: map[string]any{
			"name": "com.example/skill",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var skillOne models.SkillResponse
	require.NoError(t, json.Unmarshal(raw, &skillOne))
	assert.Equal(t, "com.example/skill", skillOne.Skill.Name)
}

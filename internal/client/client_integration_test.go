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
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/internal/registry/telemetry"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"go.opentelemetry.io/otel/metric/noop"
)

func TestClientIntegration_PingAndVersion(t *testing.T) {
	fake := servicetesting.NewFakeRegistry()
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

	fake := servicetesting.NewFakeRegistry()
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
	fake := servicetesting.NewFakeRegistry()

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

func newClientWithInProcessServer(t *testing.T, fake *servicetesting.FakeRegistry) (*Client, func()) {
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

	router.NewHumaAPI(cfg, fake, mux, metrics, versionInfo, nil, nil, routeOpts)
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

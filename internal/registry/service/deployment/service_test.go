package deployment_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockDeploymentAdapter struct {
	deployed    map[string]bool
	deployCount int
}

func newMockAdapter() *mockDeploymentAdapter {
	return &mockDeploymentAdapter{deployed: map[string]bool{}}
}

func (m *mockDeploymentAdapter) deployCallCount() int {
	return m.deployCount
}

func (m *mockDeploymentAdapter) Platform() string                 { return "mock" }
func (m *mockDeploymentAdapter) SupportedResourceTypes() []string { return []string{"agent", "mcp"} }
func (m *mockDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	m.deployed[req.ID] = true
	m.deployCount++
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}
func (m *mockDeploymentAdapter) Undeploy(_ context.Context, _ *models.Deployment) error { return nil }
func (m *mockDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, nil
}
func (m *mockDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error { return nil }
func (m *mockDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return nil, nil
}

var _ registrytypes.DeploymentPlatformAdapter = (*mockDeploymentAdapter)(nil)

func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

func newTestDeploymentService(t *testing.T) (deploymentsvc.Registry, string, string) {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	ctx := testCtx()

	agentName := "test-deploy-agent"
	agentSvc := agentsvc.New(agentsvc.Dependencies{StoreDB: testDB})
	_, err := agentSvc.PublishAgent(ctx, &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          agentName,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   "Test agent for deployment",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)

	providerID := "test-mock-provider"
	provSvc := providersvc.New(providersvc.Dependencies{Providers: testDB.Providers()})
	_, err = provSvc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       providerID,
		Name:     "Mock Provider",
		Platform: "mock",
	})
	require.NoError(t, err)

	svc := deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:   testDB,
		Providers: provSvc,
		DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"mock": newMockAdapter(),
		},
	})
	return svc, agentName, providerID
}

// newTestDeploymentServiceWithAdapter creates a deployment service wired to
// the given mock adapter so tests can inspect adapter call counts. It publishes
// a test agent with the given name/version and returns the service and provider ID.
func newTestDeploymentServiceWithAdapter(t *testing.T, adapter *mockDeploymentAdapter, agentName, agentVersion string) (deploymentsvc.Registry, string) {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	ctx := testCtx()

	agentSvc := agentsvc.New(agentsvc.Dependencies{StoreDB: testDB})
	_, err := agentSvc.PublishAgent(ctx, &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          agentName,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   "Test agent for deployment",
		},
		Version: agentVersion,
	})
	require.NoError(t, err)

	providerID := "test-mock-provider"
	provSvc := providersvc.New(providersvc.Dependencies{Providers: testDB.Providers()})
	_, err = provSvc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       providerID,
		Name:     "Mock Provider",
		Platform: "mock",
	})
	require.NoError(t, err)

	svc := deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:   testDB,
		Providers: provSvc,
		DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"mock": adapter,
		},
	})

	return svc, providerID
}

func TestApplyAgentDeployment_Create(t *testing.T) {
	ctx := testCtx()
	svc, agentName, providerID := newTestDeploymentService(t)

	dep, err := svc.ApplyAgentDeployment(ctx, agentName, "1.0.0", providerID, map[string]string{}, nil, false, false)
	require.NoError(t, err)
	require.NotNil(t, dep)
	assert.Equal(t, models.DeploymentStatusDeployed, dep.Status)
	assert.Equal(t, agentName, dep.ServerName)
	assert.Equal(t, providerID, dep.ProviderID)
}

func TestApplyAgentDeployment_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc, agentName, providerID := newTestDeploymentService(t)

	env := map[string]string{}

	dep1, err := svc.ApplyAgentDeployment(ctx, agentName, "1.0.0", providerID, env, nil, false, false)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, dep1)
	assert.Equal(t, models.DeploymentStatusDeployed, dep1.Status)

	dep2, err := svc.ApplyAgentDeployment(ctx, agentName, "1.0.0", providerID, env, nil, false, false)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, dep2)
	assert.Equal(t, dep1.ID, dep2.ID, "idempotent apply must return same deployment")

	deployments, err := svc.ListDeployments(ctx, &models.DeploymentFilter{})
	require.NoError(t, err)
	assert.Len(t, deployments, 1, "idempotent apply must not create duplicate records")
}

func TestApplyAgentDeployment_RedeploysOnEnvChange(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "redeploy-env", "1.0.0")

	dep1, err := svc.ApplyAgentDeployment(ctx, "redeploy-env", "1.0.0", providerID, map[string]string{"LOG": "info"}, nil, false, false)
	require.NoError(t, err)
	require.NotNil(t, dep1)

	deployCalls1 := mockAdapter.deployCallCount()

	// Apply with changed env and force=true — must trigger undeploy+redeploy.
	dep2, err := svc.ApplyAgentDeployment(ctx, "redeploy-env", "1.0.0", providerID, map[string]string{"LOG": "debug"}, nil, false, true)
	require.NoError(t, err)
	require.NotNil(t, dep2)

	assert.NotEqual(t, dep1.ID, dep2.ID, "env change must produce a new deployment ID")
	assert.Greater(t, mockAdapter.deployCallCount(), deployCalls1, "adapter.Deploy must be invoked again")
	assert.Equal(t, "debug", dep2.Env["LOG"], "new deployment must have updated env")
}

func TestApplyAgentDeployment_RedeploysOnProviderConfigChange(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "redeploy-cfg", "1.0.0")

	dep1, err := svc.ApplyAgentDeployment(ctx, "redeploy-cfg", "1.0.0", providerID, nil, models.JSONObject{"region": "us-west-2"}, false, false)
	require.NoError(t, err)

	dep2, err := svc.ApplyAgentDeployment(ctx, "redeploy-cfg", "1.0.0", providerID, nil, models.JSONObject{"region": "us-east-1"}, false, true)
	require.NoError(t, err)

	assert.NotEqual(t, dep1.ID, dep2.ID, "providerConfig change must produce a new deployment ID")
}

func TestApplyAgentDeployment_NoOpWhenEnvIdentical(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "noop-env", "1.0.0")

	env := map[string]string{"LOG": "info"}

	dep1, err := svc.ApplyAgentDeployment(ctx, "noop-env", "1.0.0", providerID, env, nil, false, false)
	require.NoError(t, err)

	deployCalls1 := mockAdapter.deployCallCount()

	// Apply identical request — must be no-op (no second adapter.Deploy).
	dep2, err := svc.ApplyAgentDeployment(ctx, "noop-env", "1.0.0", providerID, env, nil, false, false)
	require.NoError(t, err)

	assert.Equal(t, dep1.ID, dep2.ID, "identical apply must return same ID")
	assert.Equal(t, deployCalls1, mockAdapter.deployCallCount(), "no-op must not call adapter.Deploy again")
}

func TestApplyAgentDeployment_NoOpWhenNilVsEmptyEnv(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "noop-nilenv", "1.0.0")

	dep1, err := svc.ApplyAgentDeployment(ctx, "noop-nilenv", "1.0.0", providerID, nil, nil, false, false)
	require.NoError(t, err)

	deployCalls1 := mockAdapter.deployCallCount()

	dep2, err := svc.ApplyAgentDeployment(ctx, "noop-nilenv", "1.0.0", providerID, map[string]string{}, nil, false, false)
	require.NoError(t, err)

	assert.Equal(t, dep1.ID, dep2.ID, "nil and empty env must be treated as equal")
	assert.Equal(t, deployCalls1, mockAdapter.deployCallCount(), "must not redeploy")
}

// minimalServerJSON returns a minimal valid ServerJSON suitable for testing.
func minimalServerJSON(name, version, description string) *apiv0.ServerJSON {
	return &apiv0.ServerJSON{
		Name:        name,
		Version:     version,
		Description: description,
		Schema:      model.CurrentSchemaURL,
	}
}

// newTestDeploymentServiceWithServer creates a deployment service wired to the given
// mock adapter and publishes a test MCP server. It returns the service and provider ID.
func newTestDeploymentServiceWithServer(t *testing.T, adapter *mockDeploymentAdapter, serverName, serverVersion string) (deploymentsvc.Registry, string) {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	ctx := testCtx()

	serverSvc := serversvc.New(serversvc.Dependencies{StoreDB: testDB})
	_, err := serverSvc.PublishServer(ctx, minimalServerJSON(serverName, serverVersion, "Test server for deployment"))
	require.NoError(t, err)

	providerID := "test-mock-provider"
	provSvc := providersvc.New(providersvc.Dependencies{Providers: testDB.Providers()})
	_, err = provSvc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       providerID,
		Name:     "Mock Provider",
		Platform: "mock",
	})
	require.NoError(t, err)

	svc := deploymentsvc.New(deploymentsvc.Dependencies{
		StoreDB:   testDB,
		Providers: provSvc,
		DeploymentAdapters: map[string]registrytypes.DeploymentPlatformAdapter{
			"mock": adapter,
		},
	})

	return svc, providerID
}

func TestApplyServerDeployment_Create(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithServer(t, mockAdapter, "com.example/test-deploy-server", "1.0.0")

	env := map[string]string{"K": "V"}
	dep, err := svc.ApplyServerDeployment(ctx, "com.example/test-deploy-server", "1.0.0", providerID, env, nil, false, false)
	require.NoError(t, err)
	require.NotNil(t, dep)

	assert.Equal(t, models.DeploymentStatusDeployed, dep.Status)
	assert.Equal(t, "com.example/test-deploy-server", dep.ServerName)
	assert.Equal(t, "1.0.0", dep.Version)
	assert.Equal(t, "mcp", dep.ResourceType)
	assert.Equal(t, providerID, dep.ProviderID)
	assert.Equal(t, "V", dep.Env["K"])
}

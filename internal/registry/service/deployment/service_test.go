package deployment_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	deploymentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/deployment"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
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

func (m *mockDeploymentAdapter) Platform() string { return "mock" }
func (m *mockDeploymentAdapter) SupportedResourceTypes() []string { return []string{"agent", "mcp"} }
func (m *mockDeploymentAdapter) Deploy(_ context.Context, req *models.Deployment) (*models.DeploymentActionResult, error) {
	m.deployed[req.ID] = true
	m.deployCount++
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}
func (m *mockDeploymentAdapter) Undeploy(_ context.Context, _ *models.Deployment) error { return nil }
func (m *mockDeploymentAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) { return nil, nil }
func (m *mockDeploymentAdapter) Cancel(_ context.Context, _ *models.Deployment) error { return nil }
func (m *mockDeploymentAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) { return nil, nil }

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

func TestApplyDeployment_Create(t *testing.T) {
	ctx := testCtx()
	svc, agentName, providerID := newTestDeploymentService(t)

	dep, err := svc.ApplyDeployment(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   providerID,
		Origin:       "managed",
		Env:          map[string]string{},
	})
	require.NoError(t, err)
	require.NotNil(t, dep)
	assert.Equal(t, models.DeploymentStatusDeployed, dep.Status)
	assert.Equal(t, agentName, dep.ServerName)
	assert.Equal(t, providerID, dep.ProviderID)
}

func TestApplyDeployment_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc, agentName, providerID := newTestDeploymentService(t)

	req := &models.Deployment{
		ServerName:   agentName,
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   providerID,
		Origin:       "managed",
		Env:          map[string]string{},
	}

	dep1, err := svc.ApplyDeployment(ctx, req)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, dep1)
	assert.Equal(t, models.DeploymentStatusDeployed, dep1.Status)

	dep2, err := svc.ApplyDeployment(ctx, req)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, dep2)
	assert.Equal(t, dep1.ID, dep2.ID, "idempotent apply must return same deployment")

	deployments, err := svc.ListDeployments(ctx, &models.DeploymentFilter{})
	require.NoError(t, err)
	assert.Len(t, deployments, 1, "idempotent apply must not create duplicate records")
}

func TestApplyDeployment_RedeploysOnEnvChange(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "redeploy-env", "1.0.0")

	req1 := &models.Deployment{
		ServerName:   "redeploy-env",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   providerID,
		Env:          map[string]string{"LOG": "info"},
	}
	dep1, err := svc.ApplyDeployment(ctx, req1)
	require.NoError(t, err)
	require.NotNil(t, dep1)

	deployCalls1 := mockAdapter.deployCallCount()

	// Apply with changed env — must trigger undeploy+redeploy.
	req2 := *req1
	req2.Env = map[string]string{"LOG": "debug"}
	dep2, err := svc.ApplyDeployment(ctx, &req2)
	require.NoError(t, err)
	require.NotNil(t, dep2)

	assert.NotEqual(t, dep1.ID, dep2.ID, "env change must produce a new deployment ID")
	assert.Greater(t, mockAdapter.deployCallCount(), deployCalls1, "adapter.Deploy must be invoked again")
	assert.Equal(t, "debug", dep2.Env["LOG"], "new deployment must have updated env")
}

func TestApplyDeployment_RedeploysOnProviderConfigChange(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "redeploy-cfg", "1.0.0")

	req1 := &models.Deployment{
		ServerName:     "redeploy-cfg",
		Version:        "1.0.0",
		ResourceType:   "agent",
		ProviderID:     providerID,
		ProviderConfig: models.JSONObject{"region": "us-west-2"},
	}
	dep1, err := svc.ApplyDeployment(ctx, req1)
	require.NoError(t, err)

	req2 := *req1
	req2.ProviderConfig = models.JSONObject{"region": "us-east-1"}
	dep2, err := svc.ApplyDeployment(ctx, &req2)
	require.NoError(t, err)

	assert.NotEqual(t, dep1.ID, dep2.ID, "providerConfig change must produce a new deployment ID")
}

func TestApplyDeployment_NoOpWhenEnvIdentical(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "noop-env", "1.0.0")

	req := &models.Deployment{
		ServerName:   "noop-env",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   providerID,
		Env:          map[string]string{"LOG": "info"},
	}
	dep1, err := svc.ApplyDeployment(ctx, req)
	require.NoError(t, err)

	deployCalls1 := mockAdapter.deployCallCount()

	// Apply identical request — must be no-op (no second adapter.Deploy).
	dep2, err := svc.ApplyDeployment(ctx, req)
	require.NoError(t, err)

	assert.Equal(t, dep1.ID, dep2.ID, "identical apply must return same ID")
	assert.Equal(t, deployCalls1, mockAdapter.deployCallCount(), "no-op must not call adapter.Deploy again")
}

func TestApplyDeployment_NoOpWhenNilVsEmptyEnv(t *testing.T) {
	ctx := testCtx()
	mockAdapter := newMockAdapter()
	svc, providerID := newTestDeploymentServiceWithAdapter(t, mockAdapter, "noop-nilenv", "1.0.0")

	req1 := &models.Deployment{
		ServerName:   "noop-nilenv",
		Version:      "1.0.0",
		ResourceType: "agent",
		ProviderID:   providerID,
		Env:          nil,
	}
	dep1, err := svc.ApplyDeployment(ctx, req1)
	require.NoError(t, err)

	deployCalls1 := mockAdapter.deployCallCount()

	req2 := *req1
	req2.Env = map[string]string{}
	dep2, err := svc.ApplyDeployment(ctx, &req2)
	require.NoError(t, err)

	assert.Equal(t, dep1.ID, dep2.ID, "nil and empty env must be treated as equal")
	assert.Equal(t, deployCalls1, mockAdapter.deployCallCount(), "must not redeploy")
}

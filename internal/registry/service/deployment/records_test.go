package deployment

import (
	"context"
	"errors"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	regdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	registrytypes "github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingUndeployAdapter is a minimal platform adapter whose CleanupStale always fails.
// It implements both DeploymentPlatformAdapter and deployutil.PlatformStaleCleaner.
type failingUndeployAdapter struct {
	undeployErr error
}

func (a *failingUndeployAdapter) Platform() string                 { return "failing" }
func (a *failingUndeployAdapter) SupportedResourceTypes() []string { return []string{"agent", "mcp"} }
func (a *failingUndeployAdapter) Deploy(_ context.Context, _ *models.Deployment) (*models.DeploymentActionResult, error) {
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}
func (a *failingUndeployAdapter) Undeploy(_ context.Context, _ *models.Deployment) error {
	return a.undeployErr
}
func (a *failingUndeployAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, nil
}
func (a *failingUndeployAdapter) Cancel(_ context.Context, _ *models.Deployment) error { return nil }
func (a *failingUndeployAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return nil, nil
}

// CleanupStale makes failingUndeployAdapter satisfy deployutil.PlatformStaleCleaner.
func (a *failingUndeployAdapter) CleanupStale(_ context.Context, _ *models.Deployment) error {
	return a.undeployErr
}

var _ registrytypes.DeploymentPlatformAdapter = (*failingUndeployAdapter)(nil)

// successAdapter is a minimal platform adapter that always succeeds.
type successAdapter struct{}

func (a *successAdapter) Platform() string                 { return "success" }
func (a *successAdapter) SupportedResourceTypes() []string { return []string{"agent", "mcp"} }
func (a *successAdapter) Deploy(_ context.Context, _ *models.Deployment) (*models.DeploymentActionResult, error) {
	return &models.DeploymentActionResult{Status: models.DeploymentStatusDeployed}, nil
}
func (a *successAdapter) Undeploy(_ context.Context, _ *models.Deployment) error { return nil }
func (a *successAdapter) GetLogs(_ context.Context, _ *models.Deployment) ([]string, error) {
	return nil, nil
}
func (a *successAdapter) Cancel(_ context.Context, _ *models.Deployment) error { return nil }
func (a *successAdapter) Discover(_ context.Context, _ string) ([]*models.Deployment, error) {
	return nil, nil
}

// CleanupStale makes successAdapter satisfy deployutil.PlatformStaleCleaner.
func (a *successAdapter) CleanupStale(_ context.Context, _ *models.Deployment) error { return nil }

var _ registrytypes.DeploymentPlatformAdapter = (*successAdapter)(nil)

// newTestRegistryWithAdapter sets up a real DB, publishes a test agent, registers a
// provider with the given platform, and returns the concrete *registry wired to the
// supplied adapter.
func newTestRegistryWithAdapter(t *testing.T, adapter registrytypes.DeploymentPlatformAdapter, platform string) (*registry, string, string) {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	ctx := internaldb.WithTestSession(context.Background())

	agentName := "cleanup-test-agent"
	agentVersion := "1.0.0"
	agentSvc := agentsvc.New(agentsvc.Dependencies{StoreDB: testDB})
	_, err := agentSvc.PublishAgent(ctx, &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          agentName,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   "Test agent for cleanup",
		},
		Version: agentVersion,
	})
	require.NoError(t, err)

	providerID := "test-failing-provider"
	provSvc := providersvc.New(providersvc.Dependencies{Providers: testDB.Providers()})
	_, err = provSvc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       providerID,
		Name:     "Failing Provider",
		Platform: platform,
	})
	require.NoError(t, err)

	reg := &registry{
		deployments: testDB.Deployments(),
		tx:          testDB,
		providers:   provSvc,
		agents:      agentSvc,
		adapters: map[string]registrytypes.DeploymentPlatformAdapter{
			platform: adapter,
		},
	}
	return reg, agentName, providerID
}

func TestCleanupExistingDeploymentReturnsErrorOnPlatformUndeployFailure(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	undeployErr := errors.New("simulated platform failure")
	adapter := &failingUndeployAdapter{undeployErr: undeployErr}

	reg, agentName, providerID := newTestRegistryWithAdapter(t, adapter, "failing")

	// Seed a deployment record directly via CreateManagedDeploymentRecord.
	existing, err := reg.CreateManagedDeploymentRecord(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      "1.0.0",
		ResourceType: resourceTypeAgent,
		ProviderID:   providerID,
	})
	require.NoError(t, err)
	require.NotNil(t, existing)

	err = reg.cleanupExistingDeployment(ctx, existing)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "platform undeploy failed")
}

func TestCleanupExistingDeploymentDoesNotDeleteDBRecordOnPlatformFailure(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	undeployErr := errors.New("simulated platform failure")
	adapter := &failingUndeployAdapter{undeployErr: undeployErr}

	reg, agentName, providerID := newTestRegistryWithAdapter(t, adapter, "failing")

	// Seed a deployment record.
	existing, err := reg.CreateManagedDeploymentRecord(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      "1.0.0",
		ResourceType: resourceTypeAgent,
		ProviderID:   providerID,
	})
	require.NoError(t, err)
	require.NotNil(t, existing)

	// Platform undeploy fails — DB record must be preserved.
	_ = reg.cleanupExistingDeployment(ctx, existing)

	_, fetchErr := reg.deployments.GetDeployment(ctx, existing.ID)
	assert.NotErrorIs(t, fetchErr, regdb.ErrNotFound, "DB record must still exist after platform failure")
}

func TestCleanupExistingDeploymentDeletesDBRecordWhenPlatformUnresolvable(t *testing.T) {
	// Build a fresh DB with an agent and a provider whose platform has no registered adapter.
	testDB := internaldb.NewTestDB(t)
	ctx := internaldb.WithTestSession(context.Background())

	agentName := "orphan-cleanup-agent"
	agentSvc := agentsvc.New(agentsvc.Dependencies{StoreDB: testDB})
	_, err := agentSvc.PublishAgent(ctx, &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          agentName,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   "Test agent for orphan cleanup",
		},
		Version: "1.0.0",
	})
	require.NoError(t, err)

	orphanProviderID := "orphan-provider"
	provSvc := providersvc.New(providersvc.Dependencies{Providers: testDB.Providers()})
	_, err = provSvc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       orphanProviderID,
		Name:     "Orphan Provider",
		Platform: "nonexistent-platform", // no adapter registered for this platform
	})
	require.NoError(t, err)

	// Build a registry with an empty adapter map so "nonexistent-platform" cannot be resolved.
	reg := &registry{
		deployments: testDB.Deployments(),
		tx:          testDB,
		providers:   provSvc,
		agents:      agentSvc,
		adapters:    map[string]registrytypes.DeploymentPlatformAdapter{},
	}

	existing, err := reg.CreateManagedDeploymentRecord(ctx, &models.Deployment{
		ServerName:   agentName,
		Version:      "1.0.0",
		ResourceType: resourceTypeAgent,
		ProviderID:   orphanProviderID,
	})
	require.NoError(t, err)
	require.NotNil(t, existing)

	// cleanupExistingDeployment must succeed (no error) and delete the DB record.
	err = reg.cleanupExistingDeployment(ctx, existing)
	require.NoError(t, err, "platform unresolvable must not return an error")

	_, fetchErr := reg.deployments.GetDeployment(ctx, existing.ID)
	assert.ErrorIs(t, fetchErr, regdb.ErrNotFound, "DB record must be deleted even when platform is unresolvable")
}

func TestEnvEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b map[string]string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, map[string]string{}, true},
		{"empty vs empty", map[string]string{}, map[string]string{}, true},
		{"identical", map[string]string{"K": "V"}, map[string]string{"K": "V"}, true},
		{"value differs", map[string]string{"K": "V1"}, map[string]string{"K": "V2"}, false},
		{"key missing", map[string]string{"K": "V"}, map[string]string{}, false},
		{"extra key", map[string]string{"K": "V"}, map[string]string{"K": "V", "X": "Y"}, false},
		{"order independent", map[string]string{"A": "1", "B": "2"}, map[string]string{"B": "2", "A": "1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, envEqual(tc.a, tc.b))
		})
	}
}

func TestProviderConfigEqual(t *testing.T) {
	cases := []struct {
		name string
		a, b models.JSONObject
		want bool
	}{
		{"both nil", nil, nil, true},
		{"nil vs empty", nil, models.JSONObject{}, true},
		{"identical scalars", models.JSONObject{"k": "v"}, models.JSONObject{"k": "v"}, true},
		{"identical nested", models.JSONObject{"k": map[string]any{"x": float64(1)}}, models.JSONObject{"k": map[string]any{"x": float64(1)}}, true},
		{"differs scalar", models.JSONObject{"k": "v1"}, models.JSONObject{"k": "v2"}, false},
		{"differs nested", models.JSONObject{"k": map[string]any{"x": float64(1)}}, models.JSONObject{"k": map[string]any{"x": float64(2)}}, false},
		{"extra key", models.JSONObject{"a": "1"}, models.JSONObject{"a": "1", "b": "2"}, false},
		{"key order independent", models.JSONObject{"a": "1", "b": "2"}, models.JSONObject{"b": "2", "a": "1"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, providerConfigEqual(tc.a, tc.b))
		})
	}
}

// newTestRegistryWithSuccessAdapter is like newTestRegistryWithAdapter but uses
// a platform adapter that always succeeds, suitable for testing apply behavior.
func newTestRegistryWithSuccessAdapter(t *testing.T, agentName, agentVersion string) (*registry, string) {
	t.Helper()
	adapter := &successAdapter{}
	reg, _, providerID := newTestRegistryWithAdapter(t, adapter, "success")

	// Publish the requested agent so apply can find it.
	ctx := internaldb.WithTestSession(context.Background())
	_, err := reg.agents.PublishAgent(ctx, &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          agentName,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   "Test agent for apply drift",
		},
		Version: agentVersion,
	})
	require.NoError(t, err)

	return reg, providerID
}

func TestApplyAgentDeploymentDriftWithoutForceErrors(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	reg, providerID := newTestRegistryWithSuccessAdapter(t, "drift-agent", "1.0.0")

	// Seed a Deployed deployment with env {"A":"1"}.
	_, err := reg.ApplyAgentDeployment(ctx, "drift-agent", "1.0.0", providerID,
		map[string]string{"A": "1"}, nil, false, false)
	require.NoError(t, err)

	// Apply with changed env, force=false — expect ErrDeploymentDrift.
	_, err = reg.ApplyAgentDeployment(ctx, "drift-agent", "1.0.0", providerID,
		map[string]string{"A": "2"}, nil, false, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeploymentDrift, "expected ErrDeploymentDrift, got: %v", err)
}

func TestApplyAgentDeploymentDriftWithForceSucceeds(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	reg, providerID := newTestRegistryWithSuccessAdapter(t, "force-agent", "1.0.0")

	// Seed a Deployed deployment with env {"A":"1"}.
	dep1, err := reg.ApplyAgentDeployment(ctx, "force-agent", "1.0.0", providerID,
		map[string]string{"A": "1"}, nil, false, false)
	require.NoError(t, err)
	require.NotNil(t, dep1)

	// Apply with changed env, force=true — expect success.
	dep2, err := reg.ApplyAgentDeployment(ctx, "force-agent", "1.0.0", providerID,
		map[string]string{"A": "2"}, nil, false, true)
	require.NoError(t, err)
	require.NotNil(t, dep2)
	assert.NotEqual(t, dep1.ID, dep2.ID, "force redeploy must produce a new deployment ID")
	assert.Equal(t, "2", dep2.Env["A"], "new deployment must have updated env")
}

func TestApplyAgentDeploymentIdempotentIncludesPreferRemote(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	reg, providerID := newTestRegistryWithSuccessAdapter(t, "remote-agent", "1.0.0")

	// Seed a Deployed deployment with PreferRemote=true.
	_, err := reg.ApplyAgentDeployment(ctx, "remote-agent", "1.0.0", providerID,
		map[string]string{}, nil, true, false)
	require.NoError(t, err)

	// Apply with PreferRemote=false, force=false — expect drift error.
	_, err = reg.ApplyAgentDeployment(ctx, "remote-agent", "1.0.0", providerID,
		map[string]string{}, nil, false, false)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrDeploymentDrift, "preferRemote change should trigger drift, got: %v", err)
}

func TestApplyAgentDeploymentNoOpWhenAllEqual(t *testing.T) {
	ctx := internaldb.WithTestSession(context.Background())
	reg, providerID := newTestRegistryWithSuccessAdapter(t, "noop-agent", "1.0.0")

	env := map[string]string{"A": "1"}
	cfg := models.JSONObject{"r": "us"}

	// Seed a Deployed deployment.
	dep1, err := reg.ApplyAgentDeployment(ctx, "noop-agent", "1.0.0", providerID,
		env, cfg, true, false)
	require.NoError(t, err)
	require.NotNil(t, dep1)
	assert.Equal(t, models.DeploymentStatusDeployed, dep1.Status)

	// Apply with identical values, force=false — must return same deployment (no-op).
	dep2, err := reg.ApplyAgentDeployment(ctx, "noop-agent", "1.0.0", providerID,
		env, cfg, true, false)
	require.NoError(t, err)
	require.NotNil(t, dep2)
	assert.Equal(t, dep1.ID, dep2.ID, "identical apply must return same deployment")
}

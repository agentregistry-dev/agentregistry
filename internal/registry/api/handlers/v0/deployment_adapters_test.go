package v0

import (
	"context"
	"testing"

	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildRuntimeRequestsForProvider_ResolvesManagedDeployments(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentsFn = func(_ context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
		require.NotNil(t, filter)
		require.NotNil(t, filter.ProviderID)
		require.Equal(t, "local", *filter.ProviderID)
		require.NotNil(t, filter.Origin)
		require.Equal(t, "managed", *filter.Origin)

		return []*models.Deployment{
			{
				ID:           "dep-mcp",
				ServerName:   "acme/weather",
				Version:      "1.0.0",
				ResourceType: "mcp",
				Status:       "deployed",
				PreferRemote: true,
				Env: map[string]string{
					"ENV_A":            "1",
					"ARG_--timeout":    "45",
					"HEADER_X-API-KEY": "secret",
				},
			},
			{
				ID:           "dep-agent",
				ServerName:   "acme/assistant",
				Version:      "2.0.0",
				ResourceType: "agent",
				Status:       "pending",
				Env:          map[string]string{"KAGENT_NAMESPACE": "team-a"},
			},
			{
				ID:           "dep-failed",
				ServerName:   "acme/failed",
				Version:      "3.0.0",
				ResourceType: "mcp",
				Status:       "failed",
			},
		}, nil
	}
	reg.GetServerByNameAndVersionFn = func(_ context.Context, name, version string) (*apiv0.ServerResponse, error) {
		return &apiv0.ServerResponse{
			Server: apiv0.ServerJSON{
				Name:    name,
				Version: version,
			},
		}, nil
	}
	reg.GetAgentByNameAndVersionFn = func(_ context.Context, name, version string) (*models.AgentResponse, error) {
		require.Equal(t, "acme/assistant", name)
		require.Equal(t, "2.0.0", version)
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				Version: "2.0.0",
				AgentManifest: models.AgentManifest{
					Name: "assistant",
					McpServers: []models.McpServerType{
						{
							Type:                       "registry",
							RegistryServerName:         "acme/resolved-mcp",
							RegistryServerVersion:      "latest",
							RegistryServerPreferRemote: true,
						},
					},
				},
			},
		}, nil
	}

	serverRequests, agentRequests, err := buildRuntimeRequestsForProvider(context.Background(), reg, "local", "")
	require.NoError(t, err)

	require.Len(t, serverRequests, 1)
	assert.Equal(t, "dep-mcp", serverRequests[0].DeploymentID)
	assert.Equal(t, "acme/weather", serverRequests[0].RegistryServer.Name)
	assert.Equal(t, "1.0.0", serverRequests[0].RegistryServer.Version)
	assert.Equal(t, "1", serverRequests[0].EnvValues["ENV_A"])
	assert.NotContains(t, serverRequests[0].EnvValues, "ARG_--timeout")
	assert.NotContains(t, serverRequests[0].EnvValues, "HEADER_X-API-KEY")
	assert.Equal(t, "45", serverRequests[0].ArgValues["--timeout"])
	assert.Equal(t, "secret", serverRequests[0].HeaderValues["X-API-KEY"])
	assert.True(t, serverRequests[0].PreferRemote)

	require.Len(t, agentRequests, 1)
	assert.Equal(t, "dep-agent", agentRequests[0].DeploymentID)
	assert.Equal(t, "assistant", agentRequests[0].RegistryAgent.Name)
	assert.Equal(t, "2.0.0", agentRequests[0].RegistryAgent.Version)
	assert.Equal(t, "team-a", agentRequests[0].EnvValues["KAGENT_NAMESPACE"])
	require.Len(t, agentRequests[0].ResolvedMCPServers, 1)
	assert.Equal(t, "acme/resolved-mcp", agentRequests[0].ResolvedMCPServers[0].RegistryServer.Name)
	assert.Equal(t, "dep-agent", agentRequests[0].ResolvedMCPServers[0].DeploymentID)
	assert.Equal(t, "team-a", agentRequests[0].ResolvedMCPServers[0].EnvValues["KAGENT_NAMESPACE"])
	assert.True(t, agentRequests[0].ResolvedMCPServers[0].PreferRemote)
}

func TestBuildRuntimeRequestsForProvider_ExcludeDeploymentID(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentsFn = func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
		return []*models.Deployment{
			{
				ID:           "dep-1",
				ServerName:   "acme/weather",
				Version:      "1.0.0",
				ResourceType: "mcp",
				Status:       "deployed",
			},
		}, nil
	}
	reg.GetServerByNameAndVersionFn = func(_ context.Context, name, version string) (*apiv0.ServerResponse, error) {
		return &apiv0.ServerResponse{Server: apiv0.ServerJSON{Name: name, Version: version}}, nil
	}

	serverRequests, agentRequests, err := buildRuntimeRequestsForProvider(context.Background(), reg, "local", "dep-1")
	require.NoError(t, err)
	assert.Empty(t, serverRequests)
	assert.Empty(t, agentRequests)
}

func TestBuildRuntimeRequestsForProvider_RequiresProviderID(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	_, _, err := buildRuntimeRequestsForProvider(context.Background(), reg, "   ", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, database.ErrInvalidInput)
}

func TestBuildRuntimeRequestsForProvider_SkipsStaleMissingArtifacts(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentsFn = func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
		return []*models.Deployment{
			{
				ID:           "dep-missing",
				ServerName:   "acme/missing",
				Version:      "1.0.0",
				ResourceType: "mcp",
				Status:       "deployed",
			},
			{
				ID:           "dep-present",
				ServerName:   "acme/present",
				Version:      "2.0.0",
				ResourceType: "mcp",
				Status:       "deployed",
			},
		}, nil
	}
	reg.GetServerByNameAndVersionFn = func(_ context.Context, name, version string) (*apiv0.ServerResponse, error) {
		if name == "acme/missing" {
			return nil, database.ErrNotFound
		}
		return &apiv0.ServerResponse{
			Server: apiv0.ServerJSON{Name: name, Version: version},
		}, nil
	}

	serverRequests, agentRequests, err := buildRuntimeRequestsForProvider(context.Background(), reg, "local", "")
	require.NoError(t, err)
	assert.Len(t, serverRequests, 1)
	assert.Equal(t, "dep-present", serverRequests[0].DeploymentID)
	assert.Equal(t, "acme/present", serverRequests[0].RegistryServer.Name)
	assert.Empty(t, agentRequests)
}

func TestBuildRuntimeRequestsForProvider_SkipsStaleMissingAgentArtifacts(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetDeploymentsFn = func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
		return []*models.Deployment{
			{
				ID:           "dep-agent-missing",
				ServerName:   "acme/agent-missing",
				Version:      "1.0.0",
				ResourceType: "agent",
				Status:       "deployed",
			},
			{
				ID:           "dep-agent-present",
				ServerName:   "acme/agent-present",
				Version:      "2.0.0",
				ResourceType: "agent",
				Status:       "deployed",
			},
		}, nil
	}
	reg.GetAgentByNameAndVersionFn = func(_ context.Context, name, version string) (*models.AgentResponse, error) {
		if name == "acme/agent-missing" {
			return nil, database.ErrNotFound
		}
		return &models.AgentResponse{
			Agent: models.AgentJSON{
				Version: "2.0.0",
				AgentManifest: models.AgentManifest{
					Name: "agent-present",
				},
			},
		}, nil
	}

	serverRequests, agentRequests, err := buildRuntimeRequestsForProvider(context.Background(), reg, "local", "")
	require.NoError(t, err)
	assert.Empty(t, serverRequests)
	assert.Len(t, agentRequests, 1)
	assert.Equal(t, "dep-agent-present", agentRequests[0].DeploymentID)
	assert.Equal(t, "agent-present", agentRequests[0].RegistryAgent.Name)
}

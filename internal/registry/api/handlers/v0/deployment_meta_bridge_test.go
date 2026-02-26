package v0

import (
	"context"
	"testing"
	"time"

	servicetest "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeploymentResourceIndexFiltersInactiveStatuses(t *testing.T) {
	reg := &servicetest.FakeRegistry{
		GetDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{ID: "dep-active", ServerName: "io.test/server", ResourceType: "mcp", Status: "deployed"},
				{ID: "dep-cancelled", ServerName: "io.test/server", ResourceType: "mcp", Status: "cancelled"},
			}, nil
		},
	}

	index := deploymentResourceIndex(context.Background(), reg)
	key := deploymentResourceKey{resourceType: "mcp", resourceName: "io.test/server"}

	require.Len(t, index[key], 1)
	assert.Equal(t, "dep-active", index[key][0].ID)
}

func TestAttachServerDeploymentMetaMatchesVersionAndLatest(t *testing.T) {
	now := time.Now().UTC()
	index := map[deploymentResourceKey][]models.DeploymentSummary{
		{resourceType: "mcp", resourceName: "io.test/server"}: {
			{ID: "dep-v1", Version: "1.0.0", Status: "deployed", UpdatedAt: now},
			{ID: "dep-latest", Version: "latest", Status: "deploying", UpdatedAt: now.Add(-time.Minute)},
		},
	}

	servers := []models.ServerResponse{
		{
			Server: apiv0.ServerJSON{Name: "io.test/server", Version: "1.0.0"},
			Meta: models.ServerResponseMeta{
				Official: &apiv0.RegistryExtensions{IsLatest: false},
			},
		},
		{
			Server: apiv0.ServerJSON{Name: "io.test/server", Version: "2.0.0"},
			Meta: models.ServerResponseMeta{
				Official: &apiv0.RegistryExtensions{IsLatest: true},
			},
		},
	}

	enriched := attachServerDeploymentMeta(servers, index)
	require.NotNil(t, enriched[0].Meta.Deployments)
	require.NotNil(t, enriched[1].Meta.Deployments)
	assert.Equal(t, 1, enriched[0].Meta.Deployments.Count)
	assert.Equal(t, "dep-v1", enriched[0].Meta.Deployments.Deployments[0].ID)
	assert.Equal(t, 1, enriched[1].Meta.Deployments.Count)
	assert.Equal(t, "dep-latest", enriched[1].Meta.Deployments.Deployments[0].ID)
}

func TestAttachAgentDeploymentMetaUsesAgentResourceType(t *testing.T) {
	now := time.Now().UTC()
	index := map[deploymentResourceKey][]models.DeploymentSummary{
		{resourceType: "agent", resourceName: "planner-agent"}: {
			{ID: "dep-agent", Version: "", Status: "discovered", UpdatedAt: now},
		},
		{resourceType: "mcp", resourceName: "planner-agent"}: {
			{ID: "dep-wrong-type", Version: "", Status: "deployed", UpdatedAt: now},
		},
	}

	agents := []models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{Name: "planner-agent"},
				Version:       "1.2.3",
			},
			Meta: models.AgentResponseMeta{
				Official: &models.AgentRegistryExtensions{IsLatest: true},
			},
		},
	}

	enriched := attachAgentDeploymentMeta(agents, index)
	require.NotNil(t, enriched[0].Meta.Deployments)
	assert.Equal(t, 1, enriched[0].Meta.Deployments.Count)
	assert.Equal(t, "dep-agent", enriched[0].Meta.Deployments.Deployments[0].ID)
}

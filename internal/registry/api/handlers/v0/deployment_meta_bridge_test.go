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

func TestDeploymentResourceIndexIncludesAllStatuses(t *testing.T) {
	now := time.Now().UTC()
	reg := &servicetest.FakeRegistry{
		GetDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{ID: "dep-deploying", ServerName: "io.test/server", ResourceType: "mcp", Status: "deploying", UpdatedAt: now.Add(30 * time.Second)},
				{ID: "dep-active", ServerName: "io.test/server", ResourceType: "mcp", Status: "deployed", UpdatedAt: now},
				{ID: "dep-discovered", ServerName: "io.test/server", ResourceType: "mcp", Status: "discovered", UpdatedAt: now.Add(-30 * time.Second)},
				{ID: "dep-cancelled", ServerName: "io.test/server", ResourceType: "mcp", Status: "cancelled", UpdatedAt: now.Add(-time.Minute)},
			}, nil
		},
	}

	index := deploymentResourceIndex(context.Background(), reg)
	key := deploymentResourceKey{resourceType: "mcp", resourceName: "io.test/server"}

	require.Len(t, index[key], 4)
	assert.Equal(t, "dep-deploying", index[key][0].ID)
	assert.Equal(t, "deploying", index[key][0].Status)
	assert.Equal(t, "dep-active", index[key][1].ID)
	assert.Equal(t, "deployed", index[key][1].Status)
	assert.Equal(t, "dep-discovered", index[key][2].ID)
	assert.Equal(t, "discovered", index[key][2].Status)
	assert.Equal(t, "dep-cancelled", index[key][3].ID)
	assert.Equal(t, "cancelled", index[key][3].Status)
}

func TestAttachServerDeploymentMetaMatchesVersionAndLatest(t *testing.T) {
	now := time.Now().UTC()
	reg := &servicetest.FakeRegistry{
		GetDeploymentsFn: func(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{ID: "dep-v1", ServerName: "io.test/server", ResourceType: "mcp", Version: "1.0.0", Status: "deployed", UpdatedAt: now},
				{ID: "dep-latest", ServerName: "io.test/server", ResourceType: "mcp", Version: "latest", Status: "deployed", UpdatedAt: now.Add(-time.Minute)},
			}, nil
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

	enriched := attachServerDeploymentMeta(context.Background(), reg, servers)
	require.NotNil(t, enriched[0].Meta.Deployments)
	require.NotNil(t, enriched[1].Meta.Deployments)
	assert.Equal(t, 1, enriched[0].Meta.Deployments.Count)
	assert.Equal(t, "dep-v1", enriched[0].Meta.Deployments.Deployments[0].ID)
	assert.Equal(t, 1, enriched[1].Meta.Deployments.Count)
	assert.Equal(t, "dep-latest", enriched[1].Meta.Deployments.Deployments[0].ID)
}

package deploymentmeta

import (
	"context"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeLister struct {
	deployments []*models.Deployment
	err         error
}

func (f *fakeLister) ListDeployments(_ context.Context, _ *models.DeploymentFilter) ([]*models.Deployment, error) {
	return f.deployments, f.err
}

func TestDeploymentResourceIndexIncludesOnlyDeployedStatuses(t *testing.T) {
	now := time.Now().UTC()
	lister := &fakeLister{
		deployments: []*models.Deployment{
			{ID: "dep-deploying", ServerName: "io.test/server", ResourceType: "mcp", Status: models.DeploymentStatusDeploying, UpdatedAt: now.Add(30 * time.Second)},
			{ID: "dep-deployed", ServerName: "io.test/server", ResourceType: "mcp", Status: models.DeploymentStatusDeployed, UpdatedAt: now.Add(-15 * time.Second)},
			{ID: "dep-discovered", ServerName: "io.test/server", ResourceType: "mcp", Status: models.DeploymentStatusDiscovered, UpdatedAt: now.Add(-30 * time.Second)},
			{ID: "dep-cancelled", ServerName: "io.test/server", ResourceType: "mcp", Status: models.DeploymentStatusCancelled, UpdatedAt: now.Add(-time.Minute)},
		},
	}

	index := deploymentResourceIndex(context.Background(), lister)
	key := deploymentResourceKey{resourceType: "mcp", resourceName: "io.test/server"}

	require.Len(t, index[key], 1)
	assert.Equal(t, "dep-deployed", index[key][0].ID)
	assert.Equal(t, models.DeploymentStatusDeployed, index[key][0].Status)
}

func TestAttachServerDeploymentMetaMatchesVersionAndLatest(t *testing.T) {
	now := time.Now().UTC()
	lister := &fakeLister{
		deployments: []*models.Deployment{
			{ID: "dep-v1", ServerName: "io.test/server", ResourceType: "mcp", Version: "1.0.0", Status: models.DeploymentStatusDeployed, UpdatedAt: now},
			{ID: "dep-latest", ServerName: "io.test/server", ResourceType: "mcp", Version: "latest", Status: models.DeploymentStatusDeployed, UpdatedAt: now.Add(-time.Minute)},
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

	enriched := AttachServerDeploymentMeta(context.Background(), lister, servers)
	require.NotNil(t, enriched[0].Meta.Deployments)
	require.NotNil(t, enriched[1].Meta.Deployments)
	assert.Equal(t, 1, enriched[0].Meta.Deployments.Count)
	assert.Equal(t, "dep-v1", enriched[0].Meta.Deployments.Deployments[0].ID)
	assert.Equal(t, 1, enriched[1].Meta.Deployments.Count)
	assert.Equal(t, "dep-latest", enriched[1].Meta.Deployments.Deployments[0].ID)
}

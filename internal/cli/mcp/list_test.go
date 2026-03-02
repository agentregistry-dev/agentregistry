package mcp

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/stretchr/testify/assert"
)

func TestBuildMCPDeploymentCounts(t *testing.T) {
	deployments := []*client.DeploymentResponse{
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp"},
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp"},
		{ServerName: "acme/weather", Version: "2.0.0", ResourceType: "mcp"},
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent"},
		nil,
	}

	counts := buildMCPDeploymentCounts(deployments)
	assert.Equal(t, 2, counts["acme/weather"]["1.0.0"])
	assert.Equal(t, 1, counts["acme/weather"]["2.0.0"])
	assert.Nil(t, counts["acme/planner"])
}

func TestDeployedStatusForMCP(t *testing.T) {
	counts := map[string]map[string]int{
		"acme/weather": {
			"1.0.0": 2,
			"2.0.0": 1,
		},
	}

	assert.Equal(t, "True (2)", deployedStatusForMCP(counts, "acme/weather", "1.0.0"))
	assert.Equal(t, "True", deployedStatusForMCP(counts, "acme/weather", "2.0.0"))
	assert.Equal(t, "False", deployedStatusForMCP(counts, "acme/weather", "3.0.0"))
	assert.Equal(t, "False", deployedStatusForMCP(counts, "acme/unknown", "1.0.0"))
}

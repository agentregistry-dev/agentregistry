package mcp

import (
	"testing"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/stretchr/testify/assert"
)

func TestBuildDeploymentCounts_MCP(t *testing.T) {
	deployments := []*client.DeploymentResponse{
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp"},
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp"},
		{ServerName: "acme/weather", Version: "2.0.0", ResourceType: "mcp"},
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent"},
		nil,
	}

	counts := cliCommon.BuildDeploymentCounts(deployments, "mcp")
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

	assert.Equal(t, "True (2)", cliCommon.DeployedStatus(counts, "acme/weather", "1.0.0", false))
	assert.Equal(t, "True", cliCommon.DeployedStatus(counts, "acme/weather", "2.0.0", false))
	assert.Equal(t, "False", cliCommon.DeployedStatus(counts, "acme/weather", "3.0.0", false))
	assert.Equal(t, "False", cliCommon.DeployedStatus(counts, "acme/unknown", "1.0.0", false))
}

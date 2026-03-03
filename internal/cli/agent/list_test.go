package agent

import (
	"testing"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/stretchr/testify/assert"
)

func TestBuildDeploymentCounts_Agent(t *testing.T) {
	deployments := []*client.DeploymentResponse{
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent"},
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent"},
		{ServerName: "acme/planner", Version: "2.0.0", ResourceType: "agent"},
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp"},
		nil,
	}

	counts := cliCommon.BuildDeploymentCounts(deployments, "agent")
	assert.Equal(t, 2, counts["acme/planner"]["1.0.0"])
	assert.Equal(t, 1, counts["acme/planner"]["2.0.0"])
	assert.Nil(t, counts["acme/weather"])
}

func TestDeployedStatusForAgent(t *testing.T) {
	counts := map[string]map[string]int{
		"acme/planner": {
			"1.0.0": 2,
			"2.0.0": 1,
		},
	}

	assert.Equal(t, "True (2)", cliCommon.DeployedStatus(counts, "acme/planner", "1.0.0", true))
	assert.Equal(t, "True", cliCommon.DeployedStatus(counts, "acme/planner", "2.0.0", true))
	assert.Equal(t, "False (other versions deployed)", cliCommon.DeployedStatus(counts, "acme/planner", "3.0.0", true))
	assert.Equal(t, "False", cliCommon.DeployedStatus(counts, "acme/unknown", "1.0.0", true))
}

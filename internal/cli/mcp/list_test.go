package mcp

import (
	"bytes"
	"os"
	"testing"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestBuildDeploymentCounts_MCP(t *testing.T) {
	deployments := []*client.DeploymentResponse{
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp", Status: models.DeploymentStatusDeployed},
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp", Status: models.DeploymentStatusFailed},
		{ServerName: "acme/weather", Version: "1.0.0", ResourceType: "mcp", Status: models.DeploymentStatusDeployed},
		{ServerName: "acme/weather", Version: "2.0.0", ResourceType: "mcp", Status: models.DeploymentStatusDeployed},
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

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("creating pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w

	fn()

	w.Close()
	os.Stdout = origStdout

	var buf bytes.Buffer
	buf.ReadFrom(r)
	return buf.String()
}

func TestPrintServersTable_TypeAndPackage(t *testing.T) {
	tests := []struct {
		name        string
		servers     []*v0.ServerResponse
		wantType    string
		wantPackage string
	}{
		{
			name: "npm package shows type and identifier",
			servers: []*v0.ServerResponse{
				{
					Server: v0.ServerJSON{
						Name:    "io.github.acme/weather",
						Version: "1.0.0",
						Packages: []model.Package{
							{
								RegistryType: "npm",
								Identifier:   "@acme/weather-server",
							},
						},
					},
					Meta: v0.ResponseMeta{
						Official: &v0.RegistryExtensions{Status: model.StatusActive},
					},
				},
			},
			wantType:    "npm",
			wantPackage: "@acme/weather-server",
		},
		{
			name: "pypi package shows type and identifier",
			servers: []*v0.ServerResponse{
				{
					Server: v0.ServerJSON{
						Name:    "io.github.acme/translate",
						Version: "2.1.0",
						Packages: []model.Package{
							{
								RegistryType: "pypi",
								Identifier:   "acme-translate-mcp",
							},
						},
					},
					Meta: v0.ResponseMeta{
						Official: &v0.RegistryExtensions{Status: model.StatusActive},
					},
				},
			},
			wantType:    "pypi",
			wantPackage: "acme-translate-mcp",
		},
		{
			name: "oci package shows type and image reference",
			servers: []*v0.ServerResponse{
				{
					Server: v0.ServerJSON{
						Name:    "io.github.acme/runner",
						Version: "0.5.0",
						Packages: []model.Package{
							{
								RegistryType: "oci",
								Identifier:   "ghcr.io/acme/runner:v0.5.0",
							},
						},
					},
					Meta: v0.ResponseMeta{
						Official: &v0.RegistryExtensions{Status: model.StatusActive},
					},
				},
			},
			wantType:    "oci",
			wantPackage: "ghcr.io/acme/runner:v0.5.0",
		},
		{
			name: "remote-only server shows transport type and no package",
			servers: []*v0.ServerResponse{
				{
					Server: v0.ServerJSON{
						Name:    "io.github.acme/remote",
						Version: "1.0.0",
						Remotes: []model.Transport{
							{Type: "streamable-http", URL: "https://api.acme.com/mcp"},
						},
					},
					Meta: v0.ResponseMeta{
						Official: &v0.RegistryExtensions{Status: model.StatusActive},
					},
				},
			},
			wantType:    "streamable-http",
			wantPackage: "<none>",
		},
		{
			name: "server with no packages or remotes",
			servers: []*v0.ServerResponse{
				{
					Server: v0.ServerJSON{
						Name:    "io.github.acme/empty",
						Version: "0.0.1",
					},
					Meta: v0.ResponseMeta{
						Official: &v0.RegistryExtensions{Status: model.StatusActive},
					},
				},
			},
			wantType:    "<none>",
			wantPackage: "<none>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStdout(t, func() {
				printServersTable(tt.servers, nil)
			})

			assert.Contains(t, output, tt.wantType, "expected type value in table output")
			assert.Contains(t, output, tt.wantPackage, "expected package value in table output")
		})
	}
}

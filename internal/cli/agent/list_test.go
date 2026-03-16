package agent

import (
	"bytes"
	"os"
	"testing"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
)

func TestBuildDeploymentCounts_Agent(t *testing.T) {
	deployments := []*client.DeploymentResponse{
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent", Status: models.DeploymentStatusDeployed},
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent", Status: models.DeploymentStatusFailed},
		{ServerName: "acme/planner", Version: "1.0.0", ResourceType: "agent", Status: models.DeploymentStatusDeployed},
		{ServerName: "acme/planner", Version: "2.0.0", ResourceType: "agent", Status: models.DeploymentStatusDeployed},
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

func TestPrintAgentsTable_ImageAndRepository(t *testing.T) {
	tests := []struct {
		name       string
		agents     []*models.AgentResponse
		wantImage  string
		wantRepo   string
	}{
		{
			name: "manifest image and repository",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name:  "acme/bot",
							Image: "ghcr.io/acme/bot:latest",
						},
						Version: "1.0.0",
						Repository: &model.Repository{
							URL:    "https://github.com/acme/bot",
							Source: "git",
						},
					},
				},
			},
			wantImage: "ghcr.io/acme/bot:latest",
			wantRepo:  "https://github.com/acme/bot",
		},
		{
			name: "no image or repo shows <none>",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name: "acme/simple",
						},
						Version: "0.1.0",
					},
				},
			},
			wantImage: "<none>",
			wantRepo:  "<none>",
		},
		{
			name: "fallback to oci package identifier",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name: "acme/container",
						},
						Version: "2.0.0",
						Packages: []models.AgentPackageInfo{
							{RegistryType: "oci", Identifier: "docker.io/acme/container:2.0.0"},
						},
					},
				},
			},
			wantImage: "docker.io/acme/container:2.0.0",
			wantRepo:  "<none>",
		},
		{
			name: "fallback to docker package identifier",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name: "acme/dkr",
						},
						Version: "1.0.0",
						Packages: []models.AgentPackageInfo{
							{RegistryType: "docker", Identifier: "acme/dkr:v1"},
						},
					},
				},
			},
			wantImage: "acme/dkr:v1",
			wantRepo:  "<none>",
		},
		{
			name: "docker package with mixed casing and whitespace",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name: "acme/cased",
						},
						Version: "1.0.0",
						Packages: []models.AgentPackageInfo{
							{RegistryType: " OCI ", Identifier: "ghcr.io/acme/cased:v1"},
						},
					},
				},
			},
			wantImage: "ghcr.io/acme/cased:v1",
			wantRepo:  "<none>",
		},
		{
			name: "manifest image takes precedence over oci package",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name:  "acme/both",
							Image: "myregistry.io/acme/both:v3",
						},
						Version: "3.0.0",
						Packages: []models.AgentPackageInfo{
							{RegistryType: "oci", Identifier: "ghcr.io/acme/both:v3"},
						},
					},
				},
			},
			wantImage: "myregistry.io/acme/both:v3",
			wantRepo:  "<none>",
		},
		{
			name: "npm package is not treated as image",
			agents: []*models.AgentResponse{
				{
					Agent: models.AgentJSON{
						AgentManifest: models.AgentManifest{
							Name: "acme/jsbot",
						},
						Version: "1.0.0",
						Packages: []models.AgentPackageInfo{
							{RegistryType: "npm", Identifier: "@acme/jsbot"},
						},
					},
				},
			},
			wantImage: "<none>",
			wantRepo:  "<none>",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output := captureStdout(t, func() {
				printAgentsTable(tt.agents, nil)
			})

			assert.Contains(t, output, tt.wantImage, "expected image value in table output")
			assert.Contains(t, output, tt.wantRepo, "expected repository value in table output")
		})
	}
}

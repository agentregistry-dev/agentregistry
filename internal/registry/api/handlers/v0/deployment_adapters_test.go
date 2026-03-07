package v0

import (
	"context"
	"testing"

	platformshared "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/shared"
	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	servicetesting "github.com/agentregistry-dev/agentregistry/internal/registry/service/testing"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSplitDeploymentRuntimeInputs(t *testing.T) {
	env, args, headers := splitDeploymentRuntimeInputs(map[string]string{
		"ENV_A":            "1",
		"ARG_--timeout":    "45",
		"HEADER_X-API-KEY": "secret",
	})

	assert.Equal(t, map[string]string{"ENV_A": "1"}, env)
	assert.Equal(t, map[string]string{"--timeout": "45"}, args)
	assert.Equal(t, map[string]string{"X-API-KEY": "secret"}, headers)
}

func TestResolveAgentManifestPlatformMCPServers(t *testing.T) {
	reg := servicetesting.NewFakeRegistry()
	reg.GetServerByNameAndVersionFn = func(_ context.Context, name, version string) (*apiv0.ServerResponse, error) {
		switch name {
		case "acme/remote":
			return &apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    name,
					Version: version,
					Remotes: []model.Transport{
						{URL: "https://remote.example.com/mcp"},
					},
				},
			}, nil
		case "acme/local":
			return &apiv0.ServerResponse{
				Server: apiv0.ServerJSON{
					Name:    name,
					Version: version,
					Packages: []model.Package{
						{
							Identifier:   "ghcr.io/acme/local",
							RegistryType: "oci",
							Transport: model.Transport{
								Type: "stdio",
							},
						},
					},
				},
			}, nil
		default:
			return nil, assert.AnError
		}
	}

	platformServers, configs, pythonServers, err := resolveAgentManifestPlatformMCPServers(
		context.Background(),
		reg,
		"dep-123",
		&models.AgentManifest{
			McpServers: []models.McpServerType{
				{
					Type:                       "registry",
					RegistryServerName:         "acme/remote",
					RegistryServerVersion:      "latest",
					RegistryServerPreferRemote: true,
				},
				{
					Type:                       "registry",
					RegistryServerName:         "acme/local",
					RegistryServerVersion:      "latest",
					RegistryServerPreferRemote: false,
				},
			},
		},
		"team-a",
	)
	require.NoError(t, err)

	require.Len(t, platformServers, 2)
	assert.Equal(t, "team-a", platformServers[0].Namespace)
	assert.Equal(t, platformtypes.MCPServerTypeRemote, platformServers[0].MCPServerType)
	assert.Equal(t, platformtypes.MCPServerTypeLocal, platformServers[1].MCPServerType)

	require.Len(t, configs, 2)
	assert.Equal(t, "remote", configs[0].Type)
	assert.Equal(t, "https://remote.example.com/mcp", configs[0].URL)
	assert.Equal(t, platformshared.GenerateInternalNameForDeployment(platformServers[0].Name, "dep-123"), configs[0].Name)
	assert.Equal(t, "command", configs[1].Type)

	require.Len(t, pythonServers, 2)
	assert.Equal(t, configs[0].Name, pythonServers[0].Name)
	assert.Equal(t, configs[1].Name, pythonServers[1].Name)
}

func TestMergeAgentGatewayConfig_ReplacesOnlyScopedTargetsAndRoutes(t *testing.T) {
	existing := &platformtypes.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []platformtypes.LocalBind{{
			Port: 8080,
			Listeners: []platformtypes.LocalListener{{
				Name:     "default",
				Protocol: platformtypes.LocalListenerProtocolHTTP,
				Routes: []platformtypes.LocalRoute{
					{
						RouteName: localMCPRouteName,
						Backends: []platformtypes.RouteBackend{{
							Weight: 100,
							MCP: &platformtypes.MCPBackend{
								Targets: []platformtypes.MCPTarget{
									{Name: "keep-me"},
									{Name: "replace-me"},
								},
							},
						}},
					},
					{RouteName: "replace-me-route"},
					{RouteName: "keep-me-route"},
				},
			}},
		}},
	}
	incoming := &platformtypes.AgentGatewayConfig{
		Config: struct{}{},
		Binds: []platformtypes.LocalBind{{
			Port: 8080,
			Listeners: []platformtypes.LocalListener{{
				Name:     "default",
				Protocol: platformtypes.LocalListenerProtocolHTTP,
				Routes: []platformtypes.LocalRoute{
					{
						RouteName: localMCPRouteName,
						Backends: []platformtypes.RouteBackend{{
							Weight: 100,
							MCP: &platformtypes.MCPBackend{
								Targets: []platformtypes.MCPTarget{{Name: "replace-me"}},
							},
						}},
					},
					{RouteName: "replace-me-route"},
				},
			}},
		}},
	}

	mergeAgentGatewayConfig(existing, incoming, []string{"replace-me"}, []string{"replace-me-route"}, false, 8080)

	routes := existing.Binds[0].Listeners[0].Routes
	require.Len(t, routes, 3)
	require.Equal(t, localMCPRouteName, routes[0].RouteName)
	targets := routes[0].Backends[0].MCP.Targets
	require.Len(t, targets, 2)
	assert.Equal(t, "keep-me", targets[0].Name)
	assert.Equal(t, "replace-me", targets[1].Name)
	assert.Equal(t, "keep-me-route", routes[1].RouteName)
	assert.Equal(t, "replace-me-route", routes[2].RouteName)
}

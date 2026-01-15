package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type fakeRegistry struct {
	listDeploymentsFn        func(ctx context.Context) ([]*models.Deployment, error)
	getDeploymentFn          func(ctx context.Context, name, version string) (*models.Deployment, error)
	deployServerFn           func(ctx context.Context, name, version string, config map[string]string, preferRemote bool) (*models.Deployment, error)
	deployAgentFn            func(ctx context.Context, name, version string, config map[string]string, preferRemote bool) (*models.Deployment, error)
	updateDeploymentConfigFn func(ctx context.Context, name, version string, config map[string]string) (*models.Deployment, error)
	removeServerFn           func(ctx context.Context, name, version string) error
}

// Deployment-related methods
func (f *fakeRegistry) GetDeployments(ctx context.Context) ([]*models.Deployment, error) {
	if f.listDeploymentsFn != nil {
		return f.listDeploymentsFn(ctx)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) GetDeploymentByNameAndVersion(ctx context.Context, name, version string) (*models.Deployment, error) {
	if f.getDeploymentFn != nil {
		return f.getDeploymentFn(ctx, name, version)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) DeployServer(ctx context.Context, name, version string, config map[string]string, preferRemote bool) (*models.Deployment, error) {
	if f.deployServerFn != nil {
		return f.deployServerFn(ctx, name, version, config, preferRemote)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) DeployAgent(ctx context.Context, name, version string, config map[string]string, preferRemote bool) (*models.Deployment, error) {
	if f.deployAgentFn != nil {
		return f.deployAgentFn(ctx, name, version, config, preferRemote)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) UpdateDeploymentConfig(ctx context.Context, name, version string, config map[string]string) (*models.Deployment, error) {
	if f.updateDeploymentConfigFn != nil {
		return f.updateDeploymentConfigFn(ctx, name, version, config)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) RemoveServer(ctx context.Context, name, version string) error {
	if f.removeServerFn != nil {
		return f.removeServerFn(ctx, name, version)
	}
	return errors.New("not implemented")
}

func (f *fakeRegistry) ReconcileAll(context.Context) error { return nil }

// Stub remaining RegistryService methods
func (f *fakeRegistry) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return nil, "", errors.New("not implemented")
}
func (f *fakeRegistry) GetServerByName(context.Context, string) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetServerByNameAndVersion(context.Context, string, string, bool) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetAllVersionsByServerName(context.Context, string, bool) ([]*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) CreateServer(context.Context, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) UpdateServer(context.Context, string, string, *apiv0.ServerJSON, *string) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) StoreServerReadme(context.Context, string, string, []byte, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) GetServerReadmeLatest(context.Context, string) (*database.ServerReadme, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetServerReadmeByVersion(context.Context, string, string) (*database.ServerReadme, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) PublishServer(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) UnpublishServer(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) DeleteServer(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	return nil, "", errors.New("not implemented")
}
func (f *fakeRegistry) GetAgentByName(context.Context, string) (*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetAgentByNameAndVersion(context.Context, string, string) (*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetAllVersionsByAgentName(context.Context, string) ([]*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) CreateAgent(context.Context, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) PublishAgent(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) UnpublishAgent(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) DeleteAgent(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) ListSkills(context.Context, *database.SkillFilter, string, int) ([]*models.SkillResponse, string, error) {
	return nil, "", errors.New("not implemented")
}
func (f *fakeRegistry) GetSkillByName(context.Context, string) (*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetSkillByNameAndVersion(context.Context, string, string) (*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) GetAllVersionsBySkillName(context.Context, string) ([]*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) CreateSkill(context.Context, *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) PublishSkill(context.Context, string, string) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) UnpublishSkill(context.Context, string, string) error {
	return errors.New("not implemented")
}

func TestDeploymentTools_ListAndGet(t *testing.T) {
	ctx := context.Background()

	t.Setenv("AGENT_REGISTRY_JWT_PRIVATE_KEY", "0000000000000000000000000000000000000000000000000000000000000000")
	cfg := config.NewConfig()
	jwtMgr := auth.NewJWTManager(cfg)
	tokenResp, err := jwtMgr.GenerateTokenResponse(ctx, auth.JWTClaims{
		Permissions: []auth.Permission{{Action: auth.PermissionActionPublish, ResourcePattern: "*"}},
	})
	require.NoError(t, err)
	token := tokenResp.RegistryToken

	dep := &models.Deployment{
		ServerName:   "com.example/echo",
		Version:      "1.0.0",
		ResourceType: "mcp",
		PreferRemote: false,
		Config:       map[string]string{"ENV_FOO": "bar"},
	}

	reg := &fakeRegistry{
		listDeploymentsFn: func(ctx context.Context) ([]*models.Deployment, error) {
			return []*models.Deployment{dep}, nil
		},
		getDeploymentFn: func(ctx context.Context, name, version string) (*models.Deployment, error) {
			if name == dep.ServerName && version == dep.Version {
				return dep, nil
			}
			return nil, errors.New("not found")
		},
	}

	server := NewServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_deployments",
		Arguments: map[string]any{"auth_token": token},
	})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)

	var out struct {
		Deployments []models.Deployment `json:"deployments"`
	}
	raw, _ := json.Marshal(res.StructuredContent)
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Len(t, out.Deployments, 1)
	assert.Equal(t, dep.ServerName, out.Deployments[0].ServerName)

	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_deployment",
		Arguments: map[string]any{
			"name":       dep.ServerName,
			"version":    dep.Version,
			"auth_token": token,
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var single models.Deployment
	require.NoError(t, json.Unmarshal(raw, &single))
	assert.Equal(t, dep.ServerName, single.ServerName)
}

func TestDeploymentTools_AuthFailure(t *testing.T) {
	ctx := context.Background()
	t.Setenv("AGENT_REGISTRY_JWT_PRIVATE_KEY", "0000000000000000000000000000000000000000000000000000000000000000")
	reg := &fakeRegistry{
		listDeploymentsFn: func(ctx context.Context) ([]*models.Deployment, error) {
			return nil, nil
		},
	}
	server := NewServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer serverSession.Wait()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer clientSession.Close()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_deployments",
		Arguments: map[string]any{"auth_token": ""},
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.IsError)
	raw, _ := json.Marshal(res.Content)
	assert.Contains(t, string(raw), "bearer token")
}

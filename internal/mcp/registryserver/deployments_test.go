package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

type fakeRegistry struct {
	listDeploymentsFn        func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	getDeploymentFn          func(ctx context.Context, name, version, artifactType string) (*models.Deployment, error)
	deployServerFn           func(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error)
	deployAgentFn            func(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error)
	updateDeploymentConfigFn func(ctx context.Context, name, version, artifactType string, config map[string]string) (*models.Deployment, error)
	removeServerFn           func(ctx context.Context, name, version, artifactType string) error
}

// Deployment-related methods
func (f *fakeRegistry) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.listDeploymentsFn != nil {
		return f.listDeploymentsFn(ctx, filter)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) GetDeploymentByNameAndVersion(ctx context.Context, name, version, artifactType string) (*models.Deployment, error) {
	if f.getDeploymentFn != nil {
		return f.getDeploymentFn(ctx, name, version, artifactType)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) DeployServer(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
	if f.deployServerFn != nil {
		return f.deployServerFn(ctx, name, version, config, preferRemote, runtime)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) DeployAgent(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
	if f.deployAgentFn != nil {
		return f.deployAgentFn(ctx, name, version, config, preferRemote, runtime)
	}

	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) UpdateDeploymentConfig(ctx context.Context, name, version, artifactType string, config map[string]string) (*models.Deployment, error) {
	if f.updateDeploymentConfigFn != nil {
		return f.updateDeploymentConfigFn(ctx, name, version, artifactType, config)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeRegistry) RemoveServer(ctx context.Context, name, version, artifactType string) error {
	if f.removeServerFn != nil {
		return f.removeServerFn(ctx, name, version, artifactType)
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
func (f *fakeRegistry) UpsertServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeRegistry) UpsertAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}
func (f *fakeRegistry) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, errors.New("not implemented")
}

func TestDeploymentTools_ListAndGet(t *testing.T) {
	ctx := context.Background()

	// No authz provider configured; auth is bypassed.
	dep := &models.Deployment{
		ServerName:   "com.example/echo",
		Version:      "1.0.0",
		ResourceType: "mcp",
		PreferRemote: false,
		Config:       map[string]string{"ENV_FOO": "bar"},
	}

	reg := &fakeRegistry{
		listDeploymentsFn: func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{dep}, nil
		},
		getDeploymentFn: func(ctx context.Context, name, version, artifactType string) (*models.Deployment, error) {
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
	defer func() {
		_ = serverSession.Wait()
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientSession.Close()
	}()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_deployments",
		Arguments: map[string]any{},
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
			"serverName":   dep.ServerName,
			"version":      dep.Version,
			"resourceType": "mcp",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var single models.Deployment
	require.NoError(t, json.Unmarshal(raw, &single))
	assert.Equal(t, dep.ServerName, single.ServerName)
}

func TestDeploymentTools_NoAuthConfigured_AllowsRequests(t *testing.T) {
	ctx := context.Background()
	// No authz provider configured; auth should be bypassed.
	reg := &fakeRegistry{
		listDeploymentsFn: func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
			return []*models.Deployment{
				{ServerName: "com.example/no-auth", Version: "1.0.0", ResourceType: "mcp", Config: map[string]string{}},
			}, nil
		},
		getDeploymentFn: func(ctx context.Context, name, version, artifactType string) (*models.Deployment, error) {
			return &models.Deployment{ServerName: name, Version: version, ResourceType: "mcp", Config: map[string]string{}}, nil
		},
	}

	server := NewServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		_ = serverSession.Wait()
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientSession.Close()
	}()

	// No auth_token provided; should still succeed because JWT manager is nil.
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_deployments",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.NotNil(t, res.StructuredContent)

	raw, _ := json.Marshal(res.StructuredContent)
	var out struct {
		Deployments []models.Deployment `json:"deployments"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	require.Len(t, out.Deployments, 1)
	assert.Equal(t, "com.example/no-auth", out.Deployments[0].ServerName)

	// get_deployment without token also allowed
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_deployment",
		Arguments: map[string]any{
			"serverName":   "com.example/no-auth",
			"version":      "1.0.0",
			"resourceType": "mcp",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var single models.Deployment
	require.NoError(t, json.Unmarshal(raw, &single))
	assert.Equal(t, "com.example/no-auth", single.ServerName)
}

func TestDeploymentTools_DeployUpdateRemove(t *testing.T) {
	ctx := context.Background()
	// No authz provider -> easy happy path

	deployed := &models.Deployment{
		ServerName:   "com.example/echo",
		Version:      "1.0.0",
		ResourceType: "mcp",
		Config:       map[string]string{"ENV": "prod"},
	}
	updated := &models.Deployment{
		ServerName:   "com.example/echo",
		Version:      "1.0.0",
		ResourceType: "mcp",
		Config:       map[string]string{"ENV": "staging"},
	}
	agentDep := &models.Deployment{
		ServerName:   "com.example/agent",
		Version:      "2.0.0",
		ResourceType: "agent",
		Config:       map[string]string{"FOO": "bar"},
	}

	var removed bool
	reg := &fakeRegistry{
		deployServerFn: func(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
			return deployed, nil
		},
		deployAgentFn: func(ctx context.Context, name, version string, config map[string]string, preferRemote bool, runtime string) (*models.Deployment, error) {
			return agentDep, nil
		},
		updateDeploymentConfigFn: func(ctx context.Context, name, version, artifactType string, config map[string]string) (*models.Deployment, error) {
			return updated, nil
		},
		getDeploymentFn: func(ctx context.Context, name, version, artifactType string) (*models.Deployment, error) {
			if name == deployed.ServerName && version == deployed.Version {
				return deployed, nil
			}
			return nil, errors.New("not found")
		},
		removeServerFn: func(ctx context.Context, name, version, artifactType string) error {
			if name == deployed.ServerName && version == deployed.Version {
				removed = true
				return nil
			}
			return errors.New("not found")
		},
	}

	server := NewServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientSession.Close()
	}()

	// deploy_server
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "deploy_server",
		Arguments: map[string]any{
			"serverName": "com.example/echo",
			"version":    "1.0.0",
			"config":     map[string]string{"ENV": "prod"},
		},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var dep models.Deployment
	require.NoError(t, json.Unmarshal(raw, &dep))
	assert.Equal(t, "com.example/echo", dep.ServerName)
	assert.Equal(t, "mcp", dep.ResourceType)
	assert.Equal(t, "prod", dep.Config["ENV"])

	// deploy_agent
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "deploy_agent",
		Arguments: map[string]any{
			"serverName": "com.example/agent",
			"version":    "2.0.0",
			"config":     map[string]string{"FOO": "bar"},
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var depAgent models.Deployment
	require.NoError(t, json.Unmarshal(raw, &depAgent))
	assert.Equal(t, "agent", depAgent.ResourceType)
	assert.Equal(t, "com.example/agent", depAgent.ServerName)

	// update_deployment_config
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "update_deployment_config",
		Arguments: map[string]any{
			"serverName":   "com.example/echo",
			"version":      "1.0.0",
			"resourceType": "mcp",
			"config":       map[string]string{"ENV": "staging"},
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var depUpdated models.Deployment
	require.NoError(t, json.Unmarshal(raw, &depUpdated))
	assert.Equal(t, "staging", depUpdated.Config["ENV"])

	// remove_deployment
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "remove_deployment",
		Arguments: map[string]any{
			"serverName":   "com.example/echo",
			"version":      "1.0.0",
			"resourceType": "mcp",
		},
	})
	require.NoError(t, err)
	assert.True(t, removed)
	raw, _ = json.Marshal(res.StructuredContent)
	var delResp map[string]string
	require.NoError(t, json.Unmarshal(raw, &delResp))
	assert.Equal(t, "deleted", delResp["status"])
}

func TestDeploymentTools_FilterResourceType(t *testing.T) {
	ctx := context.Background()
	deployments := []*models.Deployment{
		{
			ServerName:   "com.example/echo",
			Version:      "1.0.0",
			ResourceType: "mcp",
			Config:       map[string]string{},
		},
		{
			ServerName:   "com.example/echo-agent",
			Version:      "2.0.0",
			ResourceType: "agent",
			Config:       map[string]string{},
		},
	}

	reg := &fakeRegistry{
		listDeploymentsFn: func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
			return deployments, nil
		},
	}
	server := NewServer(reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() {
		_ = clientSession.Close()
	}()

	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "list_deployments",
		Arguments: map[string]any{
			"resourceType": "agent",
		},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var out struct {
		Deployments []models.Deployment `json:"deployments"`
		Count       int                 `json:"count"`
	}
	require.NoError(t, json.Unmarshal(raw, &out))
	assert.Equal(t, 1, out.Count)
	require.Len(t, out.Deployments, 1)
	assert.Equal(t, "agent", out.Deployments[0].ResourceType)
	assert.Equal(t, "com.example/echo-agent", out.Deployments[0].ServerName)
}

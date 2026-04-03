package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	platformtypes "github.com/agentregistry-dev/agentregistry/internal/registry/platforms/types"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type fakeMCPRegistry struct {
	servers      []*apiv0.ServerResponse
	agents       []*models.AgentResponse
	skills       []*models.SkillResponse
	deployments  []*models.Deployment
	serverReadme *database.ServerReadme

	listAgentsFn             func(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error)
	getAgentByNameFn         func(ctx context.Context, agentName string) (*models.AgentResponse, error)
	getAgentByNameVersionFn  func(ctx context.Context, agentName, version string) (*models.AgentResponse, error)
	listServersFn            func(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error)
	getServerByNameVersionFn func(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error)
	getAllServerVersionsFn   func(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error)
	getServerReadmeLatestFn  func(ctx context.Context, serverName string) (*database.ServerReadme, error)
	getServerReadmeByVerFn   func(ctx context.Context, serverName, version string) (*database.ServerReadme, error)
	listSkillsFn             func(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error)
	getSkillByNameFn         func(ctx context.Context, skillName string) (*models.SkillResponse, error)
	getSkillByNameVersionFn  func(ctx context.Context, skillName, version string) (*models.SkillResponse, error)
	getDeploymentsFn         func(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error)
	getDeploymentByIDFn      func(ctx context.Context, id string) (*models.Deployment, error)
	deployServerFn           func(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	deployAgentFn            func(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error)
	undeployFn               func(ctx context.Context, deployment *models.Deployment) error
}

func (f *fakeMCPRegistry) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if f.listAgentsFn != nil {
		return f.listAgentsFn(ctx, filter, cursor, limit)
	}
	return f.agents, "", nil
}

func (f *fakeMCPRegistry) GetAgentByName(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if f.getAgentByNameFn != nil {
		return f.getAgentByNameFn(ctx, agentName)
	}
	if len(f.agents) > 0 {
		return f.agents[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetAgentByNameAndVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if f.getAgentByNameVersionFn != nil {
		return f.getAgentByNameVersionFn(ctx, agentName, version)
	}
	return f.GetAgentByName(ctx, agentName)
}

func (f *fakeMCPRegistry) GetAllVersionsByAgentName(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if len(f.agents) > 0 {
		return f.agents, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) CreateAgent(ctx context.Context, req *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeleteAgent(ctx context.Context, agentName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) UpsertAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) ResolveAgentManifestSkills(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.AgentSkillRef, error) {
	return nil, nil
}

func (f *fakeMCPRegistry) ResolveAgentManifestPrompts(ctx context.Context, manifest *models.AgentManifest) ([]platformtypes.ResolvedPrompt, error) {
	return nil, nil
}

func (f *fakeMCPRegistry) ListServers(ctx context.Context, filter *database.ServerFilter, cursor string, limit int) ([]*apiv0.ServerResponse, string, error) {
	if f.listServersFn != nil {
		return f.listServersFn(ctx, filter, cursor, limit)
	}
	return f.servers, "", nil
}

func (f *fakeMCPRegistry) GetServerByName(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if len(f.servers) > 0 {
		return f.servers[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetServerByNameAndVersion(ctx context.Context, serverName, version string) (*apiv0.ServerResponse, error) {
	if f.getServerByNameVersionFn != nil {
		return f.getServerByNameVersionFn(ctx, serverName, version)
	}
	if len(f.servers) > 0 {
		return f.servers[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetAllVersionsByServerName(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if f.getAllServerVersionsFn != nil {
		return f.getAllServerVersionsFn(ctx, serverName)
	}
	return f.servers, nil
}

func (f *fakeMCPRegistry) CreateServer(ctx context.Context, req *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) UpdateServer(ctx context.Context, serverName, version string, req *apiv0.ServerJSON, newStatus *string) (*apiv0.ServerResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) StoreServerReadme(ctx context.Context, serverName, version string, content []byte, contentType string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetServerReadmeLatest(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if f.getServerReadmeLatestFn != nil {
		return f.getServerReadmeLatestFn(ctx, serverName)
	}
	if f.serverReadme != nil {
		return f.serverReadme, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetServerReadmeByVersion(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if f.getServerReadmeByVerFn != nil {
		return f.getServerReadmeByVerFn(ctx, serverName, version)
	}
	return f.GetServerReadmeLatest(ctx, serverName)
}

func (f *fakeMCPRegistry) DeleteServer(ctx context.Context, serverName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) UpsertServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if f.listSkillsFn != nil {
		return f.listSkillsFn(ctx, filter, cursor, limit)
	}
	return f.skills, "", nil
}

func (f *fakeMCPRegistry) GetSkillByName(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if f.getSkillByNameFn != nil {
		return f.getSkillByNameFn(ctx, skillName)
	}
	if len(f.skills) > 0 {
		return f.skills[0], nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) GetSkillByNameAndVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if f.getSkillByNameVersionFn != nil {
		return f.getSkillByNameVersionFn(ctx, skillName, version)
	}
	return f.GetSkillByName(ctx, skillName)
}

func (f *fakeMCPRegistry) GetAllVersionsBySkillName(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if len(f.skills) > 0 {
		return f.skills, nil
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) CreateSkill(ctx context.Context, req *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeleteSkill(ctx context.Context, skillName, version string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	if f.getDeploymentsFn != nil {
		return f.getDeploymentsFn(ctx, filter)
	}
	return f.deployments, nil
}

func (f *fakeMCPRegistry) GetDeploymentByID(ctx context.Context, id string) (*models.Deployment, error) {
	if f.getDeploymentByIDFn != nil {
		return f.getDeploymentByIDFn(ctx, id)
	}
	return nil, database.ErrNotFound
}

func (f *fakeMCPRegistry) DeployServer(ctx context.Context, serverName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.deployServerFn != nil {
		return f.deployServerFn(ctx, serverName, version, config, preferRemote, providerID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) DeployAgent(ctx context.Context, agentName, version string, config map[string]string, preferRemote bool, providerID string) (*models.Deployment, error) {
	if f.deployAgentFn != nil {
		return f.deployAgentFn(ctx, agentName, version, config, preferRemote, providerID)
	}
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) RemoveDeploymentByID(ctx context.Context, id string) error {
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) CreateDeployment(ctx context.Context, req *models.Deployment) (*models.Deployment, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) UndeployDeployment(ctx context.Context, deployment *models.Deployment) error {
	if f.undeployFn != nil {
		return f.undeployFn(ctx, deployment)
	}
	return errors.New("not implemented")
}

func (f *fakeMCPRegistry) GetDeploymentLogs(ctx context.Context, deployment *models.Deployment) ([]string, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeMCPRegistry) CancelDeployment(ctx context.Context, deployment *models.Deployment) error {
	return errors.New("not implemented")
}

func TestServerTools_ListAndReadme(t *testing.T) {
	ctx := context.Background()

	readme := &database.ServerReadme{
		ServerName:  "com.example/echo",
		Version:     "1.0.0",
		Content:     []byte("# Echo"),
		ContentType: "text/markdown",
		SizeBytes:   6,
		SHA256:      []byte{0xaa, 0xbb},
		FetchedAt:   time.Now(),
	}
	reg := &fakeMCPRegistry{servers: []*apiv0.ServerResponse{
		{
			Server: apiv0.ServerJSON{
				Schema:      model.CurrentSchemaURL,
				Name:        "com.example/echo",
				Description: "Echo server",
				Title:       "Echo",
				Version:     "1.0.0",
			},
		},
	}, serverReadme: readme}

	server := NewServer(reg, reg, reg, reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	// list_servers
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_servers",
		Arguments: map[string]any{"limit": 10},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var listOut apiv0.ServerListResponse
	require.NoError(t, json.Unmarshal(raw, &listOut))
	require.Len(t, listOut.Servers, 1)
	assert.Equal(t, "com.example/echo", listOut.Servers[0].Server.Name)

	// get_server_readme
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_server_readme",
		Arguments: map[string]any{
			"name": "com.example/echo",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var readmeOut ServerReadmePayload
	require.NoError(t, json.Unmarshal(raw, &readmeOut))
	assert.Equal(t, "com.example/echo", readmeOut.Server)
	assert.Equal(t, "1.0.0", readmeOut.Version)
	assert.Equal(t, "text/markdown", readmeOut.ContentType)
	assert.Equal(t, "aabb", readmeOut.SHA256[:4])

	// get_server (defaults to latest)
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_server",
		Arguments: map[string]any{
			"name": "com.example/echo",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var serverOut apiv0.ServerListResponse
	require.NoError(t, json.Unmarshal(raw, &serverOut))
	require.Len(t, serverOut.Servers, 1)
	assert.Equal(t, "com.example/echo", serverOut.Servers[0].Server.Name)
	assert.Equal(t, "1.0.0", serverOut.Servers[0].Server.Version)
}

func TestAgentAndSkillTools_ListAndGet(t *testing.T) {
	ctx := context.Background()

	reg := &fakeMCPRegistry{agents: []*models.AgentResponse{
		{
			Agent: models.AgentJSON{
				AgentManifest: models.AgentManifest{
					Name:      "com.example/agent",
					Language:  "go",
					Framework: "none",
				},
				Title:   "Agent",
				Version: "1.0.0",
				Status:  string(model.StatusActive),
			},
		},
	}, skills: []*models.SkillResponse{
		{
			Skill: models.SkillJSON{
				Name:    "com.example/skill",
				Title:   "Skill",
				Version: "2.0.0",
				Status:  string(model.StatusActive),
			},
		},
	}}

	server := NewServer(reg, reg, reg, reg)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		require.NoError(t, serverSession.Wait())
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	// list_agents
	res, err := clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_agents",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	raw, _ := json.Marshal(res.StructuredContent)
	var agentList models.AgentListResponse
	require.NoError(t, json.Unmarshal(raw, &agentList))
	require.Len(t, agentList.Agents, 1)
	assert.Equal(t, "com.example/agent", agentList.Agents[0].Agent.Name)

	// get_agent
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_agent",
		Arguments: map[string]any{
			"name": "com.example/agent",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var agentOne models.AgentResponse
	require.NoError(t, json.Unmarshal(raw, &agentOne))
	assert.Equal(t, "com.example/agent", agentOne.Agent.Name)

	// list_skills
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name:      "list_skills",
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var skillList models.SkillListResponse
	require.NoError(t, json.Unmarshal(raw, &skillList))
	require.Len(t, skillList.Skills, 1)
	assert.Equal(t, "com.example/skill", skillList.Skills[0].Skill.Name)

	// get_skill
	res, err = clientSession.CallTool(ctx, &mcp.CallToolParams{
		Name: "get_skill",
		Arguments: map[string]any{
			"name": "com.example/skill",
		},
	})
	require.NoError(t, err)
	raw, _ = json.Marshal(res.StructuredContent)
	var skillOne models.SkillResponse
	require.NoError(t, json.Unmarshal(raw, &skillOne))
	assert.Equal(t, "com.example/skill", skillOne.Skill.Name)
}

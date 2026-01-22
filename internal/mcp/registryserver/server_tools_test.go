package registryserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/internal/cli/agent/frameworks/common"
	"github.com/agentregistry-dev/agentregistry/internal/models"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// A focused fake that only implements the read-only discovery methods.
type discoveryRegistry struct {
	servers      []*apiv0.ServerResponse
	agents       []*models.AgentResponse
	skills       []*models.SkillResponse
	serverReadme *database.ServerReadme
}

func (d *discoveryRegistry) ListServers(context.Context, *database.ServerFilter, string, int) ([]*apiv0.ServerResponse, string, error) {
	return d.servers, "", nil
}
func (d *discoveryRegistry) GetServerByName(context.Context, string) (*apiv0.ServerResponse, error) {
	if len(d.servers) > 0 {
		return d.servers[0], nil
	}
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) GetServerByNameAndVersion(context.Context, string, string, bool) (*apiv0.ServerResponse, error) {
	return d.GetServerByName(context.Background(), "")
}
func (d *discoveryRegistry) GetAllVersionsByServerName(context.Context, string, bool) ([]*apiv0.ServerResponse, error) {
	return d.servers, nil
}
func (d *discoveryRegistry) CreateServer(context.Context, *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) UpdateServer(context.Context, string, string, *apiv0.ServerJSON, *string) (*apiv0.ServerResponse, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) StoreServerReadme(context.Context, string, string, []byte, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) GetServerReadmeLatest(context.Context, string) (*database.ServerReadme, error) {
	return d.serverReadme, nil
}
func (d *discoveryRegistry) GetServerReadmeByVersion(context.Context, string, string) (*database.ServerReadme, error) {
	return d.serverReadme, nil
}
func (d *discoveryRegistry) PublishServer(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) UnpublishServer(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) DeleteServer(context.Context, string, string) error {
	return database.ErrNotFound
}

// Agents
func (d *discoveryRegistry) ListAgents(context.Context, *database.AgentFilter, string, int) ([]*models.AgentResponse, string, error) {
	return d.agents, "", nil
}
func (d *discoveryRegistry) GetAgentByName(context.Context, string) (*models.AgentResponse, error) {
	if len(d.agents) > 0 {
		return d.agents[0], nil
	}
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) GetAgentByNameAndVersion(context.Context, string, string) (*models.AgentResponse, error) {
	return d.GetAgentByName(context.Background(), "")
}
func (d *discoveryRegistry) GetAllVersionsByAgentName(context.Context, string) ([]*models.AgentResponse, error) {
	return d.agents, nil
}
func (d *discoveryRegistry) CreateAgent(context.Context, *models.AgentJSON) (*models.AgentResponse, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) PublishAgent(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) UnpublishAgent(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) DeleteAgent(context.Context, string, string) error {
	return database.ErrNotFound
}

// Skills
func (d *discoveryRegistry) ListSkills(context.Context, *database.SkillFilter, string, int) ([]*models.SkillResponse, string, error) {
	return d.skills, "", nil
}
func (d *discoveryRegistry) GetSkillByName(context.Context, string) (*models.SkillResponse, error) {
	if len(d.skills) > 0 {
		return d.skills[0], nil
	}
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) GetSkillByNameAndVersion(context.Context, string, string) (*models.SkillResponse, error) {
	return d.GetSkillByName(context.Background(), "")
}
func (d *discoveryRegistry) GetAllVersionsBySkillName(context.Context, string) ([]*models.SkillResponse, error) {
	return d.skills, nil
}
func (d *discoveryRegistry) CreateSkill(context.Context, *models.SkillJSON) (*models.SkillResponse, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) PublishSkill(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) UnpublishSkill(context.Context, string, string) error {
	return database.ErrNotFound
}

// Deployments and reconciler not used here.
func (d *discoveryRegistry) GetDeployments(context.Context) ([]*models.Deployment, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) GetDeploymentByNameAndVersion(context.Context, string, string) (*models.Deployment, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) DeployServer(context.Context, string, string, map[string]string, bool) (*models.Deployment, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) DeployAgent(context.Context, string, string, map[string]string, bool) (*models.Deployment, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) UpdateDeploymentConfig(context.Context, string, string, map[string]string) (*models.Deployment, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) RemoveServer(context.Context, string, string) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) ReconcileAll(context.Context) error { return nil }
func (d *discoveryRegistry) UpsertServerEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) GetServerEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
}
func (d *discoveryRegistry) UpsertAgentEmbedding(context.Context, string, string, *database.SemanticEmbedding) error {
	return database.ErrNotFound
}
func (d *discoveryRegistry) GetAgentEmbeddingMetadata(context.Context, string, string) (*database.SemanticEmbeddingMetadata, error) {
	return nil, database.ErrNotFound
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
	reg := &discoveryRegistry{
		servers: []*apiv0.ServerResponse{
			{
				Server: apiv0.ServerJSON{
					Schema:      model.CurrentSchemaURL,
					Name:        "com.example/echo",
					Description: "Echo server",
					Title:       "Echo",
					Version:     "1.0.0",
				},
			},
		},
		serverReadme: readme,
	}

	server := NewServer(config.NewConfig(), reg)
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

	reg := &discoveryRegistry{
		agents: []*models.AgentResponse{
			{
				Agent: models.AgentJSON{
					AgentManifest: common.AgentManifest{
						Name:      "com.example/agent",
						Language:  "go",
						Framework: "none",
					},
					Title:   "Agent",
					Version: "1.0.0",
					Status:  string(model.StatusActive),
				},
			},
		},
		skills: []*models.SkillResponse{
			{
				Skill: models.SkillJSON{
					Name:    "com.example/skill",
					Title:   "Skill",
					Version: "2.0.0",
					Status:  string(model.StatusActive),
				},
			},
		},
	}

	server := NewServer(config.NewConfig(), reg)
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

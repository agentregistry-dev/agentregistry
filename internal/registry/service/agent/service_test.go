package agent_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	agentsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/agent"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCtx returns a context with a test auth session embedded, which is
// required by the database layer for write operations (UpdateAgent etc.).
func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

// newTestAgentService creates an agent service backed by a real test DB.
func newTestAgentService(t *testing.T) agentsvc.Registry {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	return agentsvc.New(agentsvc.Dependencies{StoreDB: testDB})
}

// minimalAgentJSON returns a minimal valid AgentJSON suitable for testing.
func minimalAgentJSON(name, version, description string) *models.AgentJSON {
	return &models.AgentJSON{
		AgentManifest: models.AgentManifest{
			Name:          name,
			Image:         "ghcr.io/test/agent:v1",
			Language:      "python",
			Framework:     "adk",
			ModelProvider: "openai",
			ModelName:     "gpt-4o",
			Description:   description,
		},
		Version: version,
	}
}

func TestApplyAgent_Create(t *testing.T) {
	ctx := testCtx()
	svc := newTestAgentService(t)

	req := minimalAgentJSON("apply-create-agent", "1.0.0", "initial description")

	resp, err := svc.ApplyAgent(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, req.Name, resp.Agent.Name)
	assert.Equal(t, req.Version, resp.Agent.Version)
	assert.Equal(t, req.Description, resp.Agent.Description)

	// Verify the resource persists in the DB.
	got, err := svc.GetAgentVersion(ctx, req.Name, req.Version)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, req.Version, got.Agent.Version)
}

func TestApplyAgent_Update(t *testing.T) {
	ctx := testCtx()
	svc := newTestAgentService(t)

	const (
		name    = "apply-update-agent"
		version = "1.0.0"
	)

	// Setup: publish an initial version.
	initial := minimalAgentJSON(name, version, "original description")
	created, err := svc.PublishAgent(ctx, initial)
	require.NoError(t, err)
	require.NotNil(t, created)

	originalPublishedAt := created.Meta.Official.PublishedAt

	// Action: apply same name+version with updated description.
	updated := minimalAgentJSON(name, version, "updated description")
	resp, err := svc.ApplyAgent(ctx, updated)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Assert: description is updated.
	assert.Equal(t, "updated description", resp.Agent.Description)

	// Verify persisted state.
	got, err := svc.GetAgentVersion(ctx, name, version)
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Agent.Description)

	// published_at must be preserved; updated_at must be set (not zero).
	require.NotNil(t, got.Meta.Official)
	assert.Equal(t, originalPublishedAt.Unix(), got.Meta.Official.PublishedAt.Unix(),
		"published_at should be preserved after update")
	assert.False(t, got.Meta.Official.UpdatedAt.IsZero(),
		"updated_at should be set after an update")
}

func TestApplyAgent_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc := newTestAgentService(t)

	req := minimalAgentJSON("apply-idempotent-agent", "1.0.0", "idempotent description")

	// First apply — creates the resource.
	resp1, err := svc.ApplyAgent(ctx, req)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, resp1)

	// Second apply — same payload, should update (no error).
	resp2, err := svc.ApplyAgent(ctx, req)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, resp2)

	// Both responses describe the same resource.
	assert.Equal(t, resp1.Agent.Name, resp2.Agent.Name)
	assert.Equal(t, resp1.Agent.Version, resp2.Agent.Version)
	assert.Equal(t, resp1.Agent.Description, resp2.Agent.Description)

	// Only one version in the DB.
	versions, err := svc.GetAgentVersions(ctx, req.Name)
	require.NoError(t, err)
	assert.Len(t, versions, 1, "idempotent apply should not create duplicate versions")
}

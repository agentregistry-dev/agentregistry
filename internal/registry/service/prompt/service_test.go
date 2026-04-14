package prompt_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	promptsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/prompt"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCtx returns a context with a test auth session embedded, which is
// required by the database layer for write operations.
func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

// newTestPromptService creates a prompt service backed by a real test DB.
func newTestPromptService(t *testing.T) promptsvc.Registry {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	return promptsvc.New(promptsvc.Dependencies{StoreDB: testDB})
}

// minimalPromptJSON returns a minimal valid PromptJSON suitable for testing.
func minimalPromptJSON(name, version, content string) *models.PromptJSON {
	return &models.PromptJSON{
		Name:    name,
		Version: version,
		Content: content,
	}
}

func TestApplyPrompt_Create(t *testing.T) {
	ctx := testCtx()
	svc := newTestPromptService(t)

	req := minimalPromptJSON("apply-create-prompt", "1.0.0", "initial content")

	resp, err := svc.ApplyPrompt(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, req.Name, resp.Prompt.Name)
	assert.Equal(t, req.Version, resp.Prompt.Version)
	assert.Equal(t, req.Content, resp.Prompt.Content)

	// Verify the resource persists in the DB.
	got, err := svc.GetPromptVersion(ctx, req.Name, req.Version)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, req.Version, got.Prompt.Version)
}

func TestApplyPrompt_Update(t *testing.T) {
	ctx := testCtx()
	svc := newTestPromptService(t)

	const (
		name    = "apply-update-prompt"
		version = "1.0.0"
	)

	// Setup: publish an initial version.
	initial := minimalPromptJSON(name, version, "original content")
	created, err := svc.PublishPrompt(ctx, initial)
	require.NoError(t, err)
	require.NotNil(t, created)

	originalPublishedAt := created.Meta.Official.PublishedAt

	// Action: apply same name+version with updated content.
	updated := minimalPromptJSON(name, version, "updated content")
	resp, err := svc.ApplyPrompt(ctx, updated)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Assert: content is updated.
	assert.Equal(t, "updated content", resp.Prompt.Content)

	// Verify persisted state.
	got, err := svc.GetPromptVersion(ctx, name, version)
	require.NoError(t, err)
	assert.Equal(t, "updated content", got.Prompt.Content)

	// published_at must be preserved; updated_at must be set (not zero).
	require.NotNil(t, got.Meta.Official)
	assert.Equal(t, originalPublishedAt.Unix(), got.Meta.Official.PublishedAt.Unix(),
		"published_at should be preserved after update")
	assert.False(t, got.Meta.Official.UpdatedAt.IsZero(),
		"updated_at should be set after an update")
}

func TestApplyPrompt_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc := newTestPromptService(t)

	req := minimalPromptJSON("apply-idempotent-prompt", "1.0.0", "idempotent content")

	// First apply — creates the resource.
	resp1, err := svc.ApplyPrompt(ctx, req)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, resp1)

	// Second apply — same payload, should update (no error).
	resp2, err := svc.ApplyPrompt(ctx, req)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, resp2)

	// Both responses describe the same resource.
	assert.Equal(t, resp1.Prompt.Name, resp2.Prompt.Name)
	assert.Equal(t, resp1.Prompt.Version, resp2.Prompt.Version)
	assert.Equal(t, resp1.Prompt.Content, resp2.Prompt.Content)

	// Only one version in the DB.
	versions, err := svc.GetPromptVersions(ctx, req.Name)
	require.NoError(t, err)
	assert.Len(t, versions, 1, "idempotent apply should not create duplicate versions")
}

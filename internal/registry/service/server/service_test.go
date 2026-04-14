package server_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	serversvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/server"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCtx returns a context with a test auth session embedded, which is
// required by the database layer for write operations.
func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

// newTestServerService creates a server service backed by a real test DB.
func newTestServerService(t *testing.T) serversvc.Registry {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	return serversvc.New(serversvc.Dependencies{StoreDB: testDB})
}

// minimalServerJSON returns a minimal valid ServerJSON suitable for testing.
func minimalServerJSON(name, version, description string) *apiv0.ServerJSON {
	return &apiv0.ServerJSON{
		Name:        name,
		Version:     version,
		Description: description,
		Schema:      model.CurrentSchemaURL,
	}
}

func TestApplyServer_Create(t *testing.T) {
	ctx := testCtx()
	svc := newTestServerService(t)

	req := minimalServerJSON("com.example/apply-create-server", "1.0.0", "initial description")

	resp, err := svc.ApplyServer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, req.Name, resp.Server.Name)
	assert.Equal(t, req.Version, resp.Server.Version)
	assert.Equal(t, req.Description, resp.Server.Description)

	// Verify the resource persists in the DB.
	got, err := svc.GetServerVersion(ctx, req.Name, req.Version)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, req.Version, got.Server.Version)
}

func TestApplyServer_Update(t *testing.T) {
	ctx := testCtx()
	svc := newTestServerService(t)

	const (
		name    = "com.example/apply-update-server"
		version = "1.0.0"
	)

	// Setup: publish an initial version.
	initial := minimalServerJSON(name, version, "original description")
	created, err := svc.PublishServer(ctx, initial)
	require.NoError(t, err)
	require.NotNil(t, created)

	originalPublishedAt := created.Meta.Official.PublishedAt

	// Action: apply same name+version with updated description.
	updated := minimalServerJSON(name, version, "updated description")
	resp, err := svc.ApplyServer(ctx, updated)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Assert: description is updated.
	assert.Equal(t, "updated description", resp.Server.Description)

	// Verify persisted state.
	got, err := svc.GetServerVersion(ctx, name, version)
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Server.Description)

	// published_at must be preserved; updated_at must be set (not zero).
	require.NotNil(t, got.Meta.Official)
	assert.Equal(t, originalPublishedAt.Unix(), got.Meta.Official.PublishedAt.Unix(),
		"published_at should be preserved after update")
	assert.False(t, got.Meta.Official.UpdatedAt.IsZero(),
		"updated_at should be set after an update")
}

func TestApplyServer_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc := newTestServerService(t)

	req := minimalServerJSON("com.example/apply-idempotent-server", "1.0.0", "idempotent description")

	// First apply — creates the resource.
	resp1, err := svc.ApplyServer(ctx, req)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, resp1)

	// Second apply — same payload, should update (no error).
	resp2, err := svc.ApplyServer(ctx, req)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, resp2)

	// Both responses describe the same resource.
	assert.Equal(t, resp1.Server.Name, resp2.Server.Name)
	assert.Equal(t, resp1.Server.Version, resp2.Server.Version)
	assert.Equal(t, resp1.Server.Description, resp2.Server.Description)

	// Only one version in the DB.
	versions, err := svc.GetServerVersions(ctx, req.Name)
	require.NoError(t, err)
	assert.Len(t, versions, 1, "idempotent apply should not create duplicate versions")
}

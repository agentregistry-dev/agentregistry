package skill_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	skillsvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/skill"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testCtx returns a context with a test auth session embedded, which is
// required by the database layer for write operations.
func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

// newTestSkillService creates a skill service backed by a real test DB.
func newTestSkillService(t *testing.T) skillsvc.Registry {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	return skillsvc.New(skillsvc.Dependencies{StoreDB: testDB})
}

// minimalSkillJSON returns a minimal valid SkillJSON suitable for testing.
func minimalSkillJSON(name, version, description string) *models.SkillJSON {
	return &models.SkillJSON{
		Name:        name,
		Version:     version,
		Description: description,
	}
}

func TestApplySkill_Create(t *testing.T) {
	ctx := testCtx()
	svc := newTestSkillService(t)

	req := minimalSkillJSON("apply-create-skill", "1.0.0", "initial description")

	resp, err := svc.ApplySkill(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, req.Name, resp.Skill.Name)
	assert.Equal(t, req.Version, resp.Skill.Version)
	assert.Equal(t, req.Description, resp.Skill.Description)

	// Verify the resource persists in the DB.
	got, err := svc.GetSkillVersion(ctx, req.Name, req.Version)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, req.Version, got.Skill.Version)
}

func TestApplySkill_Update(t *testing.T) {
	ctx := testCtx()
	svc := newTestSkillService(t)

	const (
		name    = "apply-update-skill"
		version = "1.0.0"
	)

	// Setup: publish an initial version.
	initial := minimalSkillJSON(name, version, "original description")
	created, err := svc.PublishSkill(ctx, initial)
	require.NoError(t, err)
	require.NotNil(t, created)

	originalPublishedAt := created.Meta.Official.PublishedAt

	// Action: apply same name+version with updated description.
	updated := minimalSkillJSON(name, version, "updated description")
	resp, err := svc.ApplySkill(ctx, updated)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Assert: description is updated.
	assert.Equal(t, "updated description", resp.Skill.Description)

	// Verify persisted state.
	got, err := svc.GetSkillVersion(ctx, name, version)
	require.NoError(t, err)
	assert.Equal(t, "updated description", got.Skill.Description)

	// published_at must be preserved; updated_at must be set (not zero).
	require.NotNil(t, got.Meta.Official)
	assert.Equal(t, originalPublishedAt.Unix(), got.Meta.Official.PublishedAt.Unix(),
		"published_at should be preserved after update")
	assert.False(t, got.Meta.Official.UpdatedAt.IsZero(),
		"updated_at should be set after an update")
}

func TestApplySkill_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc := newTestSkillService(t)

	req := minimalSkillJSON("apply-idempotent-skill", "1.0.0", "idempotent description")

	// First apply — creates the resource.
	resp1, err := svc.ApplySkill(ctx, req)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, resp1)

	// Second apply — same payload, should update (no error).
	resp2, err := svc.ApplySkill(ctx, req)
	require.NoError(t, err, "second apply should succeed (idempotent)")
	require.NotNil(t, resp2)

	// Both responses describe the same resource.
	assert.Equal(t, resp1.Skill.Name, resp2.Skill.Name)
	assert.Equal(t, resp1.Skill.Version, resp2.Skill.Version)
	assert.Equal(t, resp1.Skill.Description, resp2.Skill.Description)

	// Only one version in the DB.
	versions, err := svc.GetSkillVersions(ctx, req.Name)
	require.NoError(t, err)
	assert.Len(t, versions, 1, "idempotent apply should not create duplicate versions")
}

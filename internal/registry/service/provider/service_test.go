package provider_test

import (
	"context"
	"testing"

	internaldb "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	providersvc "github.com/agentregistry-dev/agentregistry/internal/registry/service/provider"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testCtx() context.Context {
	return internaldb.WithTestSession(context.Background())
}

func newTestProviderService(t *testing.T) providersvc.Registry {
	t.Helper()
	testDB := internaldb.NewTestDB(t)
	// Use Providers directly (no StoreDB) so no default adapters are created.
	// All operations fall through to the DB — no platform adapter dispatch.
	return providersvc.New(providersvc.Dependencies{
		Providers: testDB.Providers(),
	})
}

func strPtr(s string) *string { return &s }

func TestApplyProvider_Create(t *testing.T) {
	ctx := testCtx()
	svc := newTestProviderService(t)

	resp, err := svc.ApplyProvider(ctx, "test-provider-create", "local", &models.UpdateProviderInput{
		Name: strPtr("My Provider"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "test-provider-create", resp.ID)
	assert.Equal(t, "My Provider", resp.Name)
	assert.Equal(t, "local", resp.Platform)

	// Verify persisted.
	got, err := svc.GetProvider(ctx, "test-provider-create")
	require.NoError(t, err)
	assert.Equal(t, "My Provider", got.Name)
}

func TestApplyProvider_Update(t *testing.T) {
	ctx := testCtx()
	svc := newTestProviderService(t)

	// Create initial provider.
	_, err := svc.RegisterProvider(ctx, &models.CreateProviderInput{
		ID:       "test-provider-update",
		Name:     "Original Name",
		Platform: "local",
	})
	require.NoError(t, err)

	// Apply with updated name.
	resp, err := svc.ApplyProvider(ctx, "test-provider-update", "local", &models.UpdateProviderInput{
		Name: strPtr("Updated Name"),
	})
	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, "Updated Name", resp.Name)

	// Verify persisted.
	got, err := svc.GetProvider(ctx, "test-provider-update")
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", got.Name)
	assert.False(t, got.CreatedAt.IsZero(), "created_at should be set")
	assert.False(t, got.UpdatedAt.IsZero(), "updated_at should be set after an update")
}

func TestApplyProvider_Idempotent(t *testing.T) {
	ctx := testCtx()
	svc := newTestProviderService(t)

	in := &models.UpdateProviderInput{Name: strPtr("Idempotent Provider")}

	resp1, err := svc.ApplyProvider(ctx, "test-provider-idempotent", "local", in)
	require.NoError(t, err, "first apply should succeed")
	require.NotNil(t, resp1)

	resp2, err := svc.ApplyProvider(ctx, "test-provider-idempotent", "local", in)
	require.NoError(t, err, "second apply should succeed")
	require.NotNil(t, resp2)

	assert.Equal(t, resp1.ID, resp2.ID)
	assert.Equal(t, resp1.Name, resp2.Name)

	// Only one provider record with our test ID should exist (not counting the default "local" provider).
	all, err := svc.ListProviders(ctx, "local")
	require.NoError(t, err)
	testIDCount := 0
	for _, p := range all {
		if p.ID == "test-provider-idempotent" {
			testIDCount++
		}
	}
	assert.Equal(t, 1, testIDCount, "idempotent apply must not create duplicate providers with the same ID")
}

func TestApplyProvider_NotFound_NoPlatform(t *testing.T) {
	ctx := testCtx()
	svc := newTestProviderService(t)

	// No platform — cannot create, should return an error.
	_, err := svc.ApplyProvider(ctx, "nonexistent-provider", "", &models.UpdateProviderInput{
		Name: strPtr("Whatever"),
	})
	require.Error(t, err)
}

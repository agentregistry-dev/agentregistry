package auth_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"testing"

	v0auth "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0/auth"
	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNoneHandler_GetAnonymousToken(t *testing.T) {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	require.NoError(t, err)

	cfg := &config.Config{
		JWTPrivateKey:       hex.EncodeToString(testSeed),
		EnableAnonymousAuth: true,
	}

	handler := v0auth.NewNoneHandler(cfg)
	ctx := context.Background()

	// Test getting anonymous token
	tokenResponse, err := handler.GetAnonymousToken(ctx)
	require.NoError(t, err)
	assert.NotEmpty(t, tokenResponse.RegistryToken)
	assert.Positive(t, tokenResponse.ExpiresAt)

	// Validate the token claims
	jwtManager := auth.NewJWTManager(cfg)
	claims, err := jwtManager.ValidateToken(ctx, tokenResponse.RegistryToken)
	require.NoError(t, err)

	// Check auth method
	assert.Equal(t, auth.MethodNone, claims.AuthMethod)
	assert.Equal(t, "anonymous", claims.AuthMethodSubject)

	expectedPermissions := []auth.Permission{
		{Action: auth.PermissionActionRead, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
		{Action: auth.PermissionActionPush, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
		{Action: auth.PermissionActionPublish, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
		{Action: auth.PermissionActionEdit, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
		{Action: auth.PermissionActionDelete, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
		{Action: auth.PermissionActionDeploy, ResourcePattern: "io.modelcontextprotocol.anonymous/*"},
	}

	assert.ElementsMatch(t, expectedPermissions, claims.Permissions, "should have all permissions")
}

package auth

import (
	"context"
)

// Authz
type AuthzProvider interface {
	// Check verifies if the session can perform the action on the resource.
	// Used for single-resource operations (get, update, delete).
	Check(ctx context.Context, s Session, verb PermissionAction, resource Resource) error
}

type Authorizer struct {
	Authz AuthzProvider
}

func (a *Authorizer) Check(ctx context.Context, verb PermissionAction, resource Resource) error {
	if a.Authz == nil {
		return nil
	}
	// Get session from context - may be nil for unauthenticated requests.
	// The AuthzProvider decides whether to allow unauthenticated access.
	s, _ := AuthSessionFrom(ctx)
	return a.Authz.Check(ctx, s, verb, resource)
}

// PublicActions defines which actions are allowed without authentication.
var PublicActions = map[PermissionAction]bool{
	PermissionActionRead: true,
	PermissionActionPull: true,
	PermissionActionRun:  true, // local runs
}

// PublicAuthzProvider implements AuthzProvider for the public version.
// It allows public read/pull/run operations without authentication,
// while requiring authentication and proper permissions for write operations.
type PublicAuthzProvider struct {
	// jwtManager handles permission checking for authenticated users
	jwtManager *JWTManager
}

// NewPublicAuthzProvider creates a new public authorization provider.
func NewPublicAuthzProvider(jwtManager *JWTManager) *PublicAuthzProvider {
	return &PublicAuthzProvider{
		jwtManager: jwtManager,
	}
}

// Check verifies if the session can perform the action on the resource.
//   - Public actions (read, pull, run) are allowed without authentication
//   - Protected actions (push, publish, edit, delete, deploy) require authentication
func (o *PublicAuthzProvider) Check(ctx context.Context, s Session, verb PermissionAction, resource Resource) error {
	// Public actions are allowed without authentication
	if PublicActions[verb] {
		return nil
	}

	// Protected actions require a session
	if s == nil {
		return ErrUnauthorized
	}

	// Delegate to JWT manager for permission checking
	return o.jwtManager.Check(ctx, s, verb, resource)
}

package auth

import (
	"context"
	"errors"
)

var (
	// ErrUnauthenticated is returned when authentication is required but not provided.
	// This should be mapped to HTTP 401 Unauthorized in handlers.
	ErrUnauthenticated = errors.New("unauthenticated")

	// ErrForbidden is returned when a user is authenticated but lacks permission.
	// This should be mapped to HTTP 403 Forbidden in handlers (or 404 to prevent info leakage).
	ErrForbidden = errors.New("forbidden")
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
		return nil // no authz provider, so allow all actions
	}
	// Get session from context - may be nil for unauthenticated requests.
	// The AuthzProvider decides whether to allow unauthenticated access.
	s, _ := AuthSessionFrom(ctx)
	return a.Authz.Check(ctx, s, verb, resource)
}

// PublicActions defines which actions are allowed without authentication (non-destructive actions).
// NOTE: In the meantime, we'll allow all actions to be performed locally without authentication.
// Once we implement better authN/authZ handling, we'll want to remove these, and just have read-only (above) actions as "public".
var PublicActions = map[PermissionAction]bool{
	PermissionActionRead:    true,
	PermissionActionPull:    true,
	PermissionActionRun:     true, // local runs
	PermissionActionPush:    true,
	PermissionActionPublish: true,
	// PermissionActionEdit:    true,
	PermissionActionDelete: true,
	PermissionActionDeploy: true,
}

// PublicAuthzProvider implements AuthzProvider for the public version.
type PublicAuthzProvider struct {
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
		return ErrUnauthenticated
	}

	// If no JWT manager is configured, allow authenticated sessions for protected actions
	if o.jwtManager == nil {
		return nil
	}

	// Delegate to JWT manager for permission checking
	return o.jwtManager.Check(ctx, s, verb, resource)
}

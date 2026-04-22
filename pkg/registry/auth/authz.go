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

// AuthzProvider defines the authorization interface.
type AuthzProvider interface {
	// Check verifies if the session can perform the action on the resource.
	// Used for single-resource operations (get, update, delete).
	Check(ctx context.Context, s Session, verb PermissionAction, resource Resource) error
	// IsRegistryAdmin checks if the session has global permissions (i.e. "*") for the registry
	// Also used by internal operations and database queries that need to bypass filtering.
	IsRegistryAdmin(ctx context.Context, s Session) bool
}

var _ AuthzProvider = &PublicAuthzProvider{}

type Authorizer struct {
	Authz AuthzProvider
}

func (a *Authorizer) Check(ctx context.Context, verb PermissionAction, resource Resource) error {
	if a.Authz == nil {
		return nil
	}
	s, _ := AuthSessionFrom(ctx)
	return a.Authz.Check(ctx, s, verb, resource)
}

func (a *Authorizer) IsRegistryAdmin(ctx context.Context) bool {
	if a.Authz == nil {
		return false
	}
	s, _ := AuthSessionFrom(ctx)
	return a.Authz.IsRegistryAdmin(ctx, s)
}

// PublicActions defines which actions are allowed without authentication.
// NOTE: All actions are currently public. Once we implement authN/authZ providers,
// we should lock this down to read-only.
var PublicActions = map[PermissionAction]bool{
	PermissionActionRead:    true,
	PermissionActionPublish: true,
	PermissionActionEdit:    true,
	PermissionActionDelete:  true,
	PermissionActionDeploy:  true,
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
func (o *PublicAuthzProvider) Check(ctx context.Context, s Session, verb PermissionAction, resource Resource) error {
	if o.IsRegistryAdmin(ctx, s) {
		return nil
	}

	if PublicActions[verb] {
		return nil
	}

	if s == nil {
		return ErrUnauthenticated
	}

	if o.jwtManager == nil {
		return nil
	}

	return o.jwtManager.Check(ctx, s, verb, resource)
}

// IsRegistryAdmin always returns true for the public provider, mirroring
// the PublicActions convention.
func (o *PublicAuthzProvider) IsRegistryAdmin(_ context.Context, _ Session) bool {
	return true
}

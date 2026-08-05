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

// PublicAuthzProvider implements the permissive default authorization policy.
// Integrators can replace it through AppOptions.AuthzProvider.
type PublicAuthzProvider struct{}

// NewPublicAuthzProvider creates a new public authorization provider.
func NewPublicAuthzProvider() *PublicAuthzProvider {
	return &PublicAuthzProvider{}
}

// Check allows every action.
func (*PublicAuthzProvider) Check(context.Context, Session, PermissionAction, Resource) error {
	return nil
}

// IsRegistryAdmin returns true because the default provider applies no restrictions.
func (*PublicAuthzProvider) IsRegistryAdmin(context.Context, Session) bool {
	return true
}

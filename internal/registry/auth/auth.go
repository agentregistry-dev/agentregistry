package auth

import (
	"context"
	"net/http"
	"net/url"

	"github.com/danielgtaylor/huma/v2"
)

var (
	ErrUnauthorized = huma.Error401Unauthorized("Unauthorized")
)

type Resource struct {
	Name string
	Type string
}

type User struct {
	Permissions []Permission
}

// Authn
type Principal struct {
	User User
}

type Session interface {
	Principal() Principal
}

type AuthnProvider interface {
	Authenticate(ctx context.Context, reqHeaders func(name string) string, query url.Values) (Session, error)
}

// Authz
type AuthzProvider interface {
	Check(ctx context.Context, s Session, verb PermissionAction, resource Resource) error
}

type Authorizer struct {
	Authz AuthzProvider
}

func (a *Authorizer) Check(ctx context.Context, verb PermissionAction, resource Resource) error {
	if a.Authz == nil {
		return nil
	}
	s, ok := AuthSessionFrom(ctx)
	if !ok {
		return ErrUnauthorized
	}
	return a.Authz.Check(ctx, s, verb, resource)
}

// context utils

type sessionKeyType struct{}

var (
	sessionKey = sessionKeyType{}
)

func AuthSessionFrom(ctx context.Context) (Session, bool) {
	v, ok := ctx.Value(sessionKey).(Session)
	return v, ok && v != nil
}

func AuthSessionTo(ctx context.Context, session Session) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

func AuthnMiddleware(authn AuthnProvider) func(ctx huma.Context, next func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		if authn == nil {
			// No auth provider configured, skip authentication
			next(ctx)
			return
		}
		url := ctx.URL()
		session, err := authn.Authenticate(ctx.Context(), ctx.Header, url.Query())
		if err != nil {
			ctx.SetStatus(http.StatusUnauthorized)
			_, _ = ctx.BodyWriter().Write([]byte("Unauthorized"))
			return
		}
		if session != nil {
			ctx = huma.WithContext(ctx, AuthSessionTo(ctx.Context(), session))
		}
		next(ctx)
	}
}

package auth

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/danielgtaylor/huma/v2"
)

type Resource struct {
	Name string
	Type PermissionArtifactType
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

// todo: the middleware config is redefined here and router. should be consolidated.
// Middleware configuration options
type middlewareConfig struct {
	skipPaths      map[string]bool // paths that should skip authentication and don't require any authorization (e.g. no need to fetch registry content)
	publicPrefixes []string        // path prefixes for paths that skip authentication, but require access to content (e.g. MCP /v0.1 requires server public listing)
}

type MiddlewareOption func(*middlewareConfig)

// WithPublicPaths marks paths that can skip authentication and require no
// authorization. Used for endpoints outside the authz model, such as health
// and metrics.
func WithSkipPaths(paths ...string) MiddlewareOption {
	return func(c *middlewareConfig) {
		for _, path := range paths {
			c.skipPaths[path] = true
		}
	}
}

// WithPublicPaths marks path prefixes as public, bypassing credential authentication
// and instead carry a PublicSession, so downstream authz hooks still see a session
// and can scope what anonymous callers may access.
func WithPublicPaths(prefixes ...string) MiddlewareOption {
	return func(c *middlewareConfig) {
		c.publicPrefixes = append(c.publicPrefixes, prefixes...)
	}
}

func AuthnMiddleware(authn AuthnProvider, options ...MiddlewareOption) func(ctx huma.Context, next func(huma.Context)) {
	config := &middlewareConfig{
		skipPaths: make(map[string]bool),
	}
	for _, option := range options {
		option(config)
	}
	return func(ctx huma.Context, next func(huma.Context)) {
		path := ctx.URL().Path

		// Skip authentication for specified paths
		// extract the last part of the path to match against skipPaths
		pathParts := strings.Split(path, "/")
		pathToMatch := "/" + pathParts[len(pathParts)-1]
		if config.skipPaths[pathToMatch] || config.skipPaths[path] {
			next(ctx)
			return
		}

		// Public paths bypass credential authentication but carry a
		// PublicSession so downstream authz hooks can scope anonymous access.
		for _, prefix := range config.publicPrefixes {
			if strings.HasPrefix(path, prefix) {
				next(huma.WithContext(ctx, WithPublicContext(ctx.Context())))
				return
			}
		}

		url := ctx.URL()
		session, err := authn.Authenticate(ctx.Context(), ctx.Header, url.Query())
		if err != nil {
			slog.Warn("authentication failed", "path", path, "error", err)
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

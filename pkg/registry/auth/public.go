package auth

import "context"

// PublicSession is the session carried by requests on public paths (see
// WithPublicPaths), e.g. the read-only MCP Registry v0.1 compatibility API.
// A public path still carries a session, so downstream authz hooks run and
// decide what an anonymous caller may see.
type PublicSession struct{}

func (s *PublicSession) Principal() Principal {
	return Principal{}
}

// IsPublicSession checks if a session is the PublicSession type.
func IsPublicSession(s Session) bool {
	_, ok := s.(*PublicSession)
	return ok
}

// WithPublicContext creates a context carrying a PublicSession.
func WithPublicContext(ctx context.Context) context.Context {
	return AuthSessionTo(ctx, &PublicSession{})
}

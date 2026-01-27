package auth

import "context"

// SystemSession is used for internal system operations (importers, reconciliation).
type SystemSession struct{}

func (s *SystemSession) Principal() Principal {
	return Principal{}
}

// IsSystemSession checks if a session is the SystemSession type.
func IsSystemSession(s Session) bool {
	_, ok := s.(*SystemSession)
	return ok
}

// WithSystemContext creates a context for internal system operations.
func WithSystemContext(ctx context.Context) context.Context {
	return AuthSessionTo(ctx, &SystemSession{})
}

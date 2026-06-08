package runtime

import (
	"context"
	"os"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
)

// Config contains the shared runtime dependencies used by command constructors.
type Config struct {
	Env             Env
	Auth            AuthProvider
	RegistryURL     *string
	RegistryToken   *string
	OnTokenResolved func(token string) error
}

func (c Config) WithDefaults() Config {
	if c.Env == nil {
		c.Env = OSEnv{}
	}
	if c.Auth == nil {
		c.Auth = NoopAuthProvider{}
	}
	return c
}

// Env abstracts environment lookup so tests and embedded CLIs can avoid
// process-global os.Getenv state.
type Env interface {
	Getenv(key string) string
}

// OSEnv is the default Env implementation.
type OSEnv struct{}

func (OSEnv) Getenv(key string) string {
	return os.Getenv(key)
}

// AuthProvider resolves a bearer token when one is available. A provider should
// return ("", nil) when auth is simply not configured or no token is stored;
// commands that need authenticated endpoints can rely on the server response.
type AuthProvider interface {
	Token(ctx context.Context) (string, error)
}

// NoopAuthProvider is the default until an embedding application supplies auth.
type NoopAuthProvider struct{}

func (NoopAuthProvider) Token(context.Context) (string, error) {
	return "", nil
}

// Deps is passed to every new-style command constructor.
//
// It is intentionally concrete and uniform. Local commands can ignore the
// fields they do not need. Registry/auth are lazy capabilities: passing this
// value to a command must not perform network or token work by itself.
type Deps struct {
	Runtime Runtime
	Auth    AuthProvider
	Kinds   *scheme.Registry
}

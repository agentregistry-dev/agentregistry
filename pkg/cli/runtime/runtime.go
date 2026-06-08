package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// RegistryTarget is the resolved registry address and bearer token for a
// command invocation.
type RegistryTarget struct {
	BaseURL string
	Token   string
}

// Runtime is the command-facing runtime contract.
type Runtime interface {
	RegistryTarget() RegistryTarget
	RegistryClient(ctx context.Context) (*client.Client, error)
}

// runtime owns per-root mutable state: flags, env-backed defaults, auth, and
// the lazily constructed registry client.
type runtime struct {
	cfg Config

	clientOnce sync.Once
	client     *client.Client
	clientErr  error
}

func New(cfg Config) Runtime {
	cfg = cfg.WithDefaults()
	return &runtime{cfg: cfg}
}

func (r *runtime) RegistryTarget() RegistryTarget {
	var baseURL string
	if r.cfg.RegistryURL != nil {
		baseURL = *r.cfg.RegistryURL
	}
	if baseURL == "" {
		baseURL = r.cfg.Env.Getenv("ARCTL_API_BASE_URL")
	}

	var token string
	if r.cfg.RegistryToken != nil {
		token = *r.cfg.RegistryToken
	}
	if token == "" {
		token = r.cfg.Env.Getenv("ARCTL_API_TOKEN")
	}

	return RegistryTarget{
		BaseURL: normalizeBaseURL(baseURL),
		Token:   token,
	}
}

// RegistryClient returns the shared registry client for this CLI invocation.
//
// Client creation is lazy and best-effort: the runtime uses a flag/env token if
// present, asks AuthProvider for a token when one is not already configured,
// and still returns an unauthenticated client when no token is found. Commands
// that do not need the registry should not call this method. Commands that need
// authenticated endpoints can rely on the server response if no usable token is
// available.
func (r *runtime) RegistryClient(ctx context.Context) (*client.Client, error) {
	r.clientOnce.Do(func() {
		target := r.RegistryTarget()
		if target.Token == "" {
			token, err := r.cfg.Auth.Token(ctx)
			if errors.Is(err, types.ErrCLINoStoredToken) {
				token = ""
				err = nil
			}
			if err != nil {
				r.clientErr = err
				return
			}
			target.Token = token
		}

		if r.cfg.OnTokenResolved != nil {
			if err := r.cfg.OnTokenResolved(target.Token); err != nil {
				r.clientErr = fmt.Errorf("calling token resolved callback: %w", err)
				return
			}
		}

		r.client = client.NewClient(target.BaseURL, target.Token)
	})

	return r.client, r.clientErr
}

func normalizeBaseURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return client.DefaultBaseURL
	}
	if strings.HasPrefix(trimmed, "http://") || strings.HasPrefix(trimmed, "https://") {
		return trimmed
	}
	return "http://" + trimmed
}

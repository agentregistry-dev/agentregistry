package runtime

import (
	"context"
	"errors"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type authProviderFunc func(context.Context) (string, error)

func (f authProviderFunc) Token(ctx context.Context) (string, error) {
	return f(ctx)
}

func TestRegistryClientAllowsMissingStoredToken(t *testing.T) {
	var resolvedToken string
	rt := New(Config{
		Auth: authProviderFunc(func(context.Context) (string, error) {
			return "", types.ErrCLINoStoredToken
		}),
		OnTokenResolved: func(token string) error {
			resolvedToken = token
			return nil
		},
	})

	client, err := rt.RegistryClient(context.Background())
	if err != nil {
		t.Fatalf("RegistryClient() error = %v, want nil", err)
	}
	if client == nil {
		t.Fatal("RegistryClient() returned nil client")
	}
	if resolvedToken != "" {
		t.Fatalf("resolved token = %q, want empty", resolvedToken)
	}
}

func TestRegistryClientReturnsAuthProviderError(t *testing.T) {
	authErr := errors.New("auth failed")
	rt := New(Config{
		Auth: authProviderFunc(func(context.Context) (string, error) {
			return "", authErr
		}),
	})

	client, err := rt.RegistryClient(context.Background())
	if !errors.Is(err, authErr) {
		t.Fatalf("RegistryClient() error = %v, want %v", err, authErr)
	}
	if client != nil {
		t.Fatal("RegistryClient() returned client for auth error")
	}
}

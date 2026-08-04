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

type envMap map[string]string

func (e envMap) Getenv(key string) string {
	return e[key]
}

func TestResolveRegistryTarget(t *testing.T) {
	authErr := errors.New("auth failed")
	tests := []struct {
		name          string
		flagURL       string
		flagToken     string
		env           envMap
		authToken     string
		authErr       error
		want          RegistryTarget
		wantErr       error
		wantAuthCalls int
	}{
		{
			name:      "flags take precedence over environment and stored token",
			flagURL:   "https://flag.example.com/v0",
			flagToken: "flag-token",
			env: envMap{
				"ARCTL_API_BASE_URL": "https://env.example.com/v0",
				"ARCTL_API_TOKEN":    "env-token",
			},
			authToken: "stored-token",
			want: RegistryTarget{
				BaseURL: "https://flag.example.com/v0",
				Token:   "flag-token",
			},
		},
		{
			name: "environment takes precedence over stored token",
			env: envMap{
				"ARCTL_API_BASE_URL": "env.example.com",
				"ARCTL_API_TOKEN":    "env-token",
			},
			authToken: "stored-token",
			want: RegistryTarget{
				BaseURL: "http://env.example.com",
				Token:   "env-token",
			},
		},
		{
			name:          "stored token is used when flag and environment are empty",
			authToken:     "stored-token",
			wantAuthCalls: 1,
			want: RegistryTarget{
				BaseURL: "http://localhost:12121/v0",
				Token:   "stored-token",
			},
		},
		{
			name:          "missing stored token allows unauthenticated target",
			authErr:       types.ErrCLINoStoredToken,
			wantAuthCalls: 1,
			want: RegistryTarget{
				BaseURL: "http://localhost:12121/v0",
			},
		},
		{
			name:          "auth provider error is returned",
			authErr:       authErr,
			wantErr:       authErr,
			wantAuthCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			authCalls := 0
			resolvedTokens := make([]string, 0, 1)
			rt := New(Config{
				Env:           tt.env,
				RegistryURL:   &tt.flagURL,
				RegistryToken: &tt.flagToken,
				Auth: authProviderFunc(func(context.Context) (string, error) {
					authCalls++
					return tt.authToken, tt.authErr
				}),
				OnTokenResolved: func(token string) error {
					resolvedTokens = append(resolvedTokens, token)
					return nil
				},
			})

			got, err := rt.ResolveRegistryTarget(context.Background())
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ResolveRegistryTarget() error = %v, want %v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("ResolveRegistryTarget() = %#v, want %#v", got, tt.want)
			}
			if authCalls != tt.wantAuthCalls {
				t.Fatalf("AuthProvider.Token() calls = %d, want %d", authCalls, tt.wantAuthCalls)
			}
			if tt.wantErr != nil {
				if len(resolvedTokens) != 0 {
					t.Fatalf("OnTokenResolved() tokens = %q, want no calls", resolvedTokens)
				}
				return
			}
			if len(resolvedTokens) != 1 || resolvedTokens[0] != tt.want.Token {
				t.Fatalf("OnTokenResolved() tokens = %q, want [%q]", resolvedTokens, tt.want.Token)
			}
		})
	}
}

func TestResolveRegistryTargetReturnsCallbackError(t *testing.T) {
	callbackErr := errors.New("callback failed")
	flagToken := "flag-token"
	rt := New(Config{
		RegistryToken: &flagToken,
		OnTokenResolved: func(string) error {
			return callbackErr
		},
	})

	got, err := rt.ResolveRegistryTarget(context.Background())
	if !errors.Is(err, callbackErr) {
		t.Fatalf("ResolveRegistryTarget() error = %v, want %v", err, callbackErr)
	}
	if got != (RegistryTarget{}) {
		t.Fatalf("ResolveRegistryTarget() = %#v, want empty target", got)
	}
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

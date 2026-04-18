package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
	"github.com/spf13/cobra"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", "http://localhost:12121/v0"},
		{"blank", "  ", "http://localhost:12121/v0"},
		{"already http", "http://localhost:8080", "http://localhost:8080"},
		{"already https", "https://api.example.com", "https://api.example.com"},
		{"no scheme", "localhost:12121", "http://localhost:12121"},
		{"no scheme trimmed", "  api.example.com  ", "http://api.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBaseURL(tt.raw)
			if got != tt.want {
				t.Errorf("normalizeBaseURL(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestPreRunBehavior(t *testing.T) {
	root := &cobra.Command{Use: "arctl"}

	agentCmd := &cobra.Command{Use: "agent"}
	initCmd := &cobra.Command{Use: "init"}
	buildCmd := &cobra.Command{Use: "build"}
	listCmd := &cobra.Command{Use: "list"}
	agentCmd.AddCommand(initCmd)
	agentCmd.AddCommand(buildCmd)
	agentCmd.AddCommand(listCmd)

	mcpCmd := &cobra.Command{Use: "mcp"}
	mcpInitCmd := &cobra.Command{Use: "init"}
	mcpBuildCmd := &cobra.Command{Use: "build"}
	mcpAddToolCmd := &cobra.Command{Use: "add-tool"}
	mcpCmd.AddCommand(mcpInitCmd)
	mcpCmd.AddCommand(mcpBuildCmd)
	mcpCmd.AddCommand(mcpAddToolCmd)

	skillCmd := &cobra.Command{Use: "skill"}
	skillInitCmd := &cobra.Command{Use: "init"}
	skillBuildCmd := &cobra.Command{Use: "build"}
	skillCmd.AddCommand(skillInitCmd)
	skillCmd.AddCommand(skillBuildCmd)

	// Subcommand under "mcp init" (e.g. arctl mcp init python mymcp)
	initPythonCmd := &cobra.Command{Use: "python"}
	mcpInitCmd.AddCommand(initPythonCmd)

	configureCmd := &cobra.Command{Use: "configure"}
	completionCmd := &cobra.Command{Use: "completion"}
	zshCompletionCmd := &cobra.Command{Use: "zsh"}
	completionCmd.AddCommand(zshCompletionCmd)
	versionCmd := &cobra.Command{Use: "version"}
	root.AddCommand(agentCmd, mcpCmd, skillCmd, configureCmd, completionCmd, versionCmd)

	tests := []struct {
		name     string
		cmd      *cobra.Command
		wantSkip bool
	}{
		{"agent init", initCmd, true},
		{"agent build", buildCmd, true},
		{"mcp init", mcpInitCmd, true},
		{"mcp build", mcpBuildCmd, true},
		{"mcp add-tool", mcpAddToolCmd, true},
		{"skill init", skillInitCmd, true},
		{"skill build", skillBuildCmd, true},
		{"configure", configureCmd, true},
		{"completion", completionCmd, true},
		{"completion zsh", zshCompletionCmd, true},
		{"version", versionCmd, true},
		{"mcp init python (subcommand of init)", initPythonCmd, true},
		{"agent list", listCmd, false},
		{"nil cmd", nil, false},
		{"top-level command with parent", agentCmd, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSkip := preRunBehavior(tt.cmd)
			if gotSkip != tt.wantSkip {
				t.Errorf("preRunBehavior() = %v, want %v", gotSkip, tt.wantSkip)
			}
		})
	}
}

func TestShouldSkipTokenResolution(t *testing.T) {
	root := &cobra.Command{Use: "arctl"}

	// Command with annotation
	annotatedCmd := &cobra.Command{
		Use:         "no-auth-cmd",
		Annotations: map[string]string{AnnotationSkipTokenResolution: "true"},
	}
	// Child inherits from annotated parent
	childOfAnnotated := &cobra.Command{Use: "child"}
	annotatedCmd.AddCommand(childOfAnnotated)

	// Child explicitly opts back in to resolving token (overrides parent)
	childOptIn := &cobra.Command{
		Use:         "secure-child",
		Annotations: map[string]string{AnnotationSkipTokenResolution: "false"},
	}
	annotatedCmd.AddCommand(childOptIn)

	// Grandchild of opt-in child (no annotation — inherits "false" from childOptIn)
	grandchild := &cobra.Command{Use: "grandchild"}
	childOptIn.AddCommand(grandchild)

	// Command without annotation
	normalCmd := &cobra.Command{Use: "normal-cmd"}

	root.AddCommand(annotatedCmd, normalCmd)

	tests := []struct {
		name     string
		cmd      *cobra.Command
		wantSkip bool
	}{
		{"annotated command", annotatedCmd, true},
		{"child inherits from annotated parent", childOfAnnotated, true},
		{"child overrides parent with false", childOptIn, false},
		{"grandchild inherits false from nearest parent", grandchild, false},
		{"command without annotation", normalCmd, false},
		{"root command", root, false},
		{"nil command", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipTokenResolution(tt.cmd)
			if got != tt.wantSkip {
				t.Errorf("shouldSkipTokenResolution() = %v, want %v", got, tt.wantSkip)
			}
		})
	}
}

func TestResolveRegistryTarget(t *testing.T) {
	env := map[string]string{
		"ARCTL_API_BASE_URL": "http://env.example.com",
		"ARCTL_API_TOKEN":    "env-token",
	}
	getEnv := func(key string) string { return env[key] }

	tests := []struct {
		name        string
		flagURL     string
		flagToken   string
		wantBaseURL string
		wantToken   string
	}{
		{"flags override env", "http://flag.example.com", "flag-token", "http://flag.example.com", "flag-token"},
		{"env only", "", "", "http://env.example.com", "env-token"},
		{"flag URL only", "http://flag.example.com", "", "http://flag.example.com", "env-token"},
		{"flag token only", "", "flag-token", "http://env.example.com", "flag-token"},
		{"no scheme in URL", "env.example.com", "t", "http://env.example.com", "t"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			registryURL = tt.flagURL
			registryToken = tt.flagToken
			defer func() {
				registryURL = ""
				registryToken = ""
			}()

			gotBase, gotToken := resolveRegistryTarget(getEnv)
			if gotBase != tt.wantBaseURL || gotToken != tt.wantToken {
				t.Errorf("resolveRegistryTarget() = (%q, %q), want (%q, %q)", gotBase, gotToken, tt.wantBaseURL, tt.wantToken)
			}
		})
	}
}

func TestConfigure(t *testing.T) {
	opts := CLIOptions{
		ClientFactory: func(_ context.Context, u, tok string) (*client.Client, error) {
			return client.NewClient(u, tok), nil
		},
	}
	Configure(opts)
	defer Configure(CLIOptions{}) // reset
	if cliOptions.ClientFactory == nil {
		t.Error("Configure: expected ClientFactory to be set")
	}
}

func TestRoot(t *testing.T) {
	cmd := Root()
	if cmd == nil {
		t.Fatal("Root() returned nil")
		return
	}
	if cmd.Use != "arctl" {
		t.Errorf("Root().Use = %q, want %q", cmd.Use, "arctl")
	}
}

func TestPreRunSetup(t *testing.T) {
	ctx := context.Background()
	baseURL := "http://localhost:12121/v0"
	token := "test-token"

	// Mock client factory that returns a dummy client (no network)
	dummyClient := client.NewClient(baseURL, token)
	clientFactory := func(_ context.Context, u, tok string) (*client.Client, error) {
		return client.NewClient(u, tok), nil
	}

	// Use a dummy command for testing, since some code paths may access cmd.Root() for token provider
	mockCmd := &cobra.Command{Use: "test"}

	oldOpts := cliOptions
	defer func() { Configure(oldOpts) }()
	Configure(CLIOptions{
		ClientFactory: clientFactory,
	})

	t.Run("basic_client_creation", func(t *testing.T) {
		c, err := preRunSetup(ctx, mockCmd, baseURL, token)
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if c == nil {
			t.Fatal("preRunSetup: expected client")
		}
	})

	t.Run("token_provider_supplies_token", func(t *testing.T) {
		var mockTokenProviderFactory = func(_ *cobra.Command) (types.CLITokenProvider, error) {
			return &mockTokenProvider{token: "authn-token"}, nil
		}

		var authnToken string
		Configure(CLIOptions{
			TokenProviderFactory: mockTokenProviderFactory,
			ClientFactory: func(_ context.Context, u, tok string) (*client.Client, error) {
				authnToken = tok
				return dummyClient, nil
			},
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, "")
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if authnToken != "authn-token" {
			t.Errorf("expected token from TokenProvider, got %q", authnToken)
		}
	})

	t.Run("skip_token_resolution_annotation_skips_token_resolution", func(t *testing.T) {
		tokenResolutionCalled := false
		var mockTokenProviderFactory = func(_ *cobra.Command) (types.CLITokenProvider, error) {
			tokenResolutionCalled = true
			return &mockTokenProvider{token: "should-not-be-used"}, nil
		}

		var clientToken string
		Configure(CLIOptions{
			TokenProviderFactory: mockTokenProviderFactory,
			ClientFactory: func(_ context.Context, u, tok string) (*client.Client, error) {
				clientToken = tok
				return client.NewClient(u, tok), nil
			},
		})
		defer func() { Configure(oldOpts) }()

		annotatedCmd := &cobra.Command{
			Use:         "skip-auth",
			Annotations: map[string]string{AnnotationSkipTokenResolution: "true"},
		}

		c, err := preRunSetup(ctx, annotatedCmd, baseURL, "")
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if c == nil {
			t.Fatal("preRunSetup: expected client")
		}
		if tokenResolutionCalled {
			t.Error("expected token provider to NOT be called when SkipTokenResolution annotation is set")
		}
		if clientToken != "" {
			t.Errorf("expected empty token, got %q", clientToken)
		}
	})

	t.Run("skip_token_resolution_annotation_still_uses_explicit_token", func(t *testing.T) {
		var clientToken string
		Configure(CLIOptions{
			TokenProviderFactory: func(_ *cobra.Command) (types.CLITokenProvider, error) {
				t.Fatal("token provider should not be called when explicit token is provided")
				return nil, nil
			},
			ClientFactory: func(_ context.Context, u, tok string) (*client.Client, error) {
				clientToken = tok
				return client.NewClient(u, tok), nil
			},
		})
		defer func() { Configure(oldOpts) }()

		annotatedCmd := &cobra.Command{
			Use:         "skip-auth",
			Annotations: map[string]string{AnnotationSkipTokenResolution: "true"},
		}

		c, err := preRunSetup(ctx, annotatedCmd, baseURL, "explicit-token")
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if c == nil {
			t.Fatal("preRunSetup: expected client")
		}
		if clientToken != "explicit-token" {
			t.Errorf("expected explicit-token, got %q", clientToken)
		}
	})

	t.Run("token_provider_error", func(t *testing.T) {
		tokenProviderErr := errors.New("auth failed")
		var mockTokenProviderFactory = func(_ *cobra.Command) (types.CLITokenProvider, error) {
			return &mockTokenProvider{err: tokenProviderErr}, nil
		}

		Configure(CLIOptions{
			TokenProviderFactory: mockTokenProviderFactory,
			ClientFactory:        clientFactory,
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, "")
		if err == nil {
			t.Fatal("expected error from TokenProvider")
		}
		if !errors.Is(err, tokenProviderErr) {
			t.Errorf("expected auth error (wrapped), got %v", err)
		}
	})

	t.Run("token_resolved_callback_success", func(t *testing.T) {
		var resolvedToken string
		Configure(CLIOptions{
			ClientFactory:   clientFactory,
			OnTokenResolved: func(tok string) error { resolvedToken = tok; return nil },
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, token)
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if resolvedToken != token {
			t.Errorf("expected OnTokenResolved to receive token %q, got %q", token, resolvedToken)
		}
	})

	t.Run("token_resolved_callback_error", func(t *testing.T) {
		callbackErr := errors.New("callback failed")
		Configure(CLIOptions{
			ClientFactory:   clientFactory,
			OnTokenResolved: func(tok string) error { return callbackErr },
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, token)
		if err == nil {
			t.Fatal("expected error from OnTokenResolved callback")
		}
		if !errors.Is(err, callbackErr) {
			t.Errorf("expected callback error (wrapped), got %v", err)
		}
	})

	t.Run("client_factory_error", func(t *testing.T) {
		clientErr := errors.New("client failed")
		Configure(CLIOptions{
			ClientFactory: func(_ context.Context, _, _ string) (*client.Client, error) {
				return nil, clientErr
			},
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, token)
		if err == nil {
			t.Fatal("expected error from ClientFactory")
		}
	})

	t.Run("client_factory_error_includes_url", func(t *testing.T) {
		clientErr := errors.New("connection refused")
		Configure(CLIOptions{
			ClientFactory: func(_ context.Context, _, _ string) (*client.Client, error) {
				return nil, clientErr
			},
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, mockCmd, baseURL, token)
		if err == nil {
			t.Fatal("expected error from ClientFactory")
		}
		if !errors.Is(err, clientErr) {
			t.Errorf("expected wrapped client error, got %v", err)
		}
		if !strings.Contains(err.Error(), baseURL) {
			t.Errorf("error should include the registry URL, got: %s", err.Error())
		}
	})
}

// mockTokenProvider for unit tests.
type mockTokenProvider struct {
	token string
	err   error
}

func (m *mockTokenProvider) Token(context.Context) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.token, nil
}

var _ types.CLITokenProvider = (*mockTokenProvider)(nil)

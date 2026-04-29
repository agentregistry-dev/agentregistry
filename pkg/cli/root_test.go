package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/annotations"
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
	// Build a synthetic command tree mirroring the current (declarative) CLI
	// surface: top-level init/build, agent/run, mcp/{run,add-tool}, skill/pull,
	// plus helper commands (configure, completion, version).
	root := &cobra.Command{Use: "arctl"}

	// Top-level declarative commands (no API client needed).
	initCmd := &cobra.Command{Use: "init"}
	buildCmd := &cobra.Command{Use: "build"}
	// Subcommand of top-level "init" (e.g. arctl init mcp fastmcp-python NAME).
	initMCPCmd := &cobra.Command{Use: "mcp"}
	initCmd.AddCommand(initMCPCmd)

	// agent/mcp/skill parents keep only run-time / add-tool / pull children.
	agentCmd := &cobra.Command{Use: "agent"}
	agentRunCmd := &cobra.Command{Use: "run"}
	agentCmd.AddCommand(agentRunCmd)

	mcpCmd := &cobra.Command{Use: "mcp"}
	mcpRunCmd := &cobra.Command{Use: "run"}
	mcpAddToolCmd := &cobra.Command{Use: "add-tool"}
	mcpCmd.AddCommand(mcpRunCmd)
	mcpCmd.AddCommand(mcpAddToolCmd)

	skillCmd := &cobra.Command{Use: "skill"}
	skillPullCmd := &cobra.Command{Use: "pull"}
	skillCmd.AddCommand(skillPullCmd)

	configureCmd := &cobra.Command{Use: "configure"}
	completionCmd := &cobra.Command{Use: "completion"}
	zshCompletionCmd := &cobra.Command{Use: "zsh"}
	completionCmd.AddCommand(zshCompletionCmd)
	versionCmd := &cobra.Command{Use: "version"}
	root.AddCommand(initCmd, buildCmd, agentCmd, mcpCmd, skillCmd, configureCmd, completionCmd, versionCmd)

	tests := []struct {
		name     string
		cmd      *cobra.Command
		wantSkip bool
	}{
		// Top-level declarative init/build skip setup (no API client).
		{"init", initCmd, true},
		{"build", buildCmd, true},
		{"init mcp (subcommand of init)", initMCPCmd, true},
		// mcp add-tool runs locally, no API client.
		{"mcp add-tool", mcpAddToolCmd, true},
		// Helper commands skip setup.
		{"configure", configureCmd, true},
		{"completion", completionCmd, true},
		{"completion zsh", zshCompletionCmd, true},
		// version goes through pre-run using AnnotationOptionalRegistry to have an optional registry connection
		{"version", versionCmd, false},
		// Run/pull/etc. need the API client.
		{"agent run", agentRunCmd, false},
		{"mcp run", mcpRunCmd, false},
		{"skill pull", skillPullCmd, false},
		// Edge cases.
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

func TestHasAnnotation(t *testing.T) {
	key := "random-annotation"

	root := &cobra.Command{Use: "arctl"}

	// Command with annotation
	annotatedCmd := &cobra.Command{
		Use:         "no-auth-cmd",
		Annotations: map[string]string{key: "true"},
	}
	// Child inherits from annotated parent
	childInherits := &cobra.Command{Use: "child"}
	annotatedCmd.AddCommand(childInherits)

	// Child explicitly opts back in (overrides parent)
	childOptIn := &cobra.Command{
		Use:         "secure-child",
		Annotations: map[string]string{key: "false"},
	}
	annotatedCmd.AddCommand(childOptIn)

	// Grandchild of opt-in child (no annotation, should inherits "false" from childOptIn)
	grandchild := &cobra.Command{Use: "grandchild"}
	childOptIn.AddCommand(grandchild)

	// Command without annotation
	normalCmd := &cobra.Command{Use: "normal-cmd"}

	root.AddCommand(annotatedCmd, normalCmd)

	tests := []struct {
		name string
		cmd  *cobra.Command
		want bool
	}{
		{"annotated command", annotatedCmd, true},
		{"child inherits from annotated parent", childInherits, true},
		{"child overrides parent with false", childOptIn, false},
		{"grandchild inherits false from nearest parent", grandchild, false},
		{"command without annotation", normalCmd, false},
		{"root command", root, false},
		{"nil command", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasAnnotation(tt.cmd, key)
			if got != tt.want {
				t.Errorf("hasAnnotation() = %v, want %v", got, tt.want)
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
			Annotations: map[string]string{annotations.AnnotationSkipTokenResolution: "true"},
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
			Annotations: map[string]string{annotations.AnnotationSkipTokenResolution: "true"},
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

	// optional-registry annotation: pre-run still resolves token + URL, but
	// only soft-fails for registry connectivity/client creation failures;
	// token resolution errors must still propagate.
	optionalRegistryCmd := &cobra.Command{
		Use:         "version",
		Annotations: map[string]string{annotations.AnnotationOptionalRegistry: "true"},
	}

	t.Run("optional_registry_connectivity_soft_fail", func(t *testing.T) {
		Configure(CLIOptions{
			ClientFactory: func(_ context.Context, _, _ string) (*client.Client, error) {
				return nil, errors.New("connection refused")
			},
		})
		defer func() { Configure(oldOpts) }()

		c, err := preRunSetup(ctx, optionalRegistryCmd, baseURL, token)
		if err != nil {
			t.Fatalf("expected soft-fail, got error: %v", err)
		}
		if c == nil {
			t.Fatal("expected non-nil client from soft-fail path")
		}
	})

	t.Run("optional_registry_does_not_swallow_token_resolution_error", func(t *testing.T) {
		// A failure with token resolution should still propagate, because optional registry is strictly about connectivity.
		tokenErr := errors.New("token store unreadable")
		Configure(CLIOptions{
			TokenProviderFactory: func(_ *cobra.Command) (types.CLITokenProvider, error) {
				return &mockTokenProvider{err: tokenErr}, nil
			},
			ClientFactory: clientFactory,
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, optionalRegistryCmd, baseURL, "")
		if !errors.Is(err, tokenErr) {
			t.Errorf("expected wrapped token error, got %v", err)
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

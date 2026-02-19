package cli

import (
	"context"
	"errors"
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

func TestParseRegistryURL(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		wantHost string
		wantNil  bool
	}{
		{"empty", "", "localhost", false},
		{"blank", "  ", "localhost", false},
		{"with path", "http://localhost:12121/v0", "localhost", false},
		{"host only", "api.example.com", "api.example.com", false},
		{"with port", "http://host:9999", "host", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRegistryURL(tt.raw)
			if tt.wantNil {
				if got != nil {
					t.Errorf("parseRegistryURL(%q) = %v, want nil", tt.raw, got)
				}
				return
			}
			if got == nil {
				t.Errorf("parseRegistryURL(%q) = nil, want host %q", tt.raw, tt.wantHost)
				return
			}
			if got.Hostname() != tt.wantHost {
				t.Errorf("parseRegistryURL(%q).Hostname() = %q, want %q", tt.raw, got.Hostname(), tt.wantHost)
			}
		})
	}
}

func TestPreRunBehavior(t *testing.T) {
	agentCmd := &cobra.Command{Use: "agent"}
	initCmd := &cobra.Command{Use: "init"}
	agentCmd.AddCommand(initCmd)

	mcpCmd := &cobra.Command{Use: "mcp"}
	mcpInitCmd := &cobra.Command{Use: "init"}
	mcpCmd.AddCommand(mcpInitCmd)

	listCmd := &cobra.Command{Use: "list"}
	agentCmd.AddCommand(listCmd)

	skillCmd := &cobra.Command{Use: "skill"}
	skillInitCmd := &cobra.Command{Use: "init"}
	skillCmd.AddCommand(skillInitCmd)

	// Subcommand under "mcp init" (e.g. arctl mcp init python mymcp)
	initPythonCmd := &cobra.Command{Use: "python"}
	mcpInitCmd.AddCommand(initPythonCmd)

	tests := []struct {
		name          string
		cmd           *cobra.Command
		baseURL       string
		wantSkip      bool
		wantAutoStart bool
	}{
		// Skip setup for init commands (baseURL irrelevant)
		{"agent init", initCmd, "http://localhost:12121", true, false},
		{"mcp init", mcpInitCmd, "http://localhost:12121", true, false},
		{"skill init", skillInitCmd, "http://localhost:12121", true, false},
		{"mcp init python (subcommand of init)", initPythonCmd, "http://localhost:12121", true, false},
		// No skip for other commands; auto-start depends on URL
		{"agent list localhost:12121", listCmd, "http://localhost:12121", false, true},
		{"agent list other port", listCmd, "http://localhost:8080", false, false},
		{"agent list remote", listCmd, "http://api.example.com:12121", false, false},
		{"default port 127.0.0.1", listCmd, "http://127.0.0.1:12121", false, true},
		{"default port with path", listCmd, "http://localhost:12121/v0", false, true},
		{"empty defaults to localhost:12121", listCmd, "", false, true},
		{"nil cmd", nil, "http://localhost:12121", false, true},
		{"nil parent (root-level)", agentCmd, "http://localhost:12121", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSkip, gotAutoStart := preRunBehavior(tt.cmd, tt.baseURL)
			if gotSkip != tt.wantSkip || gotAutoStart != tt.wantAutoStart {
				t.Errorf("preRunBehavior(%v, %q) = (skip=%v, autoStart=%v), want (skip=%v, autoStart=%v)",
					tt.cmd != nil && tt.cmd.Name() != "", tt.baseURL, gotSkip, gotAutoStart, tt.wantSkip, tt.wantAutoStart)
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

func TestParseRegistryURL_EmptyHost(t *testing.T) {
	// URL like "http://" or "://" might return empty host - ensure we don't panic
	u := parseRegistryURL("http://")
	if u != nil {
		// Parser may still return a URL with empty host
		_ = u.Hostname()
	}
}

func TestConfigure(t *testing.T) {
	opts := CLIOptions{
		DaemonManager: &mockDaemonManager{running: true},
	}
	Configure(opts)
	defer Configure(CLIOptions{}) // reset
	if cliOptions.DaemonManager == nil {
		t.Error("Configure: expected DaemonManager to be set")
	}
}

func TestRoot(t *testing.T) {
	cmd := Root()
	if cmd == nil {
		t.Fatal("Root() returned nil")
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

	// Mock daemon that is already running (so Start is not called)
	dm := &mockDaemonManager{running: true}

	oldOpts := cliOptions
	defer func() { Configure(oldOpts) }()
	Configure(CLIOptions{
		DaemonManager: dm,
		ClientFactory: clientFactory,
	})

	t.Run("no_auto_start_skips_daemon", func(t *testing.T) {
		c, err := preRunSetup(ctx, baseURL, token, false)
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if c == nil {
			t.Fatal("preRunSetup: expected client")
		}
		if dm.startCalled {
			t.Error("expected daemon Start not to be called when autoStartDaemon is false")
		}
	})

	t.Run("authn_provider_supplies_token", func(t *testing.T) {
		var authnToken string
		Configure(CLIOptions{
			DaemonManager: dm,
			AuthnProvider: &mockAuthnProvider{token: "authn-token"},
			ClientFactory: func(_ context.Context, u, tok string) (*client.Client, error) {
				authnToken = tok
				return dummyClient, nil
			},
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, baseURL, "", true)
		if err != nil {
			t.Fatalf("preRunSetup: %v", err)
		}
		if authnToken != "authn-token" {
			t.Errorf("expected token from AuthnProvider, got %q", authnToken)
		}
	})

	t.Run("authn_provider_error", func(t *testing.T) {
		authnErr := errors.New("auth failed")
		Configure(CLIOptions{
			DaemonManager: dm,
			AuthnProvider: &mockAuthnProvider{err: authnErr},
			ClientFactory: clientFactory,
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, baseURL, "", false)
		if err == nil {
			t.Fatal("expected error from AuthnProvider")
		}
		if !errors.Is(err, authnErr) {
			t.Errorf("expected auth error (wrapped), got %v", err)
		}
	})

	t.Run("client_factory_error", func(t *testing.T) {
		clientErr := errors.New("client failed")
		Configure(CLIOptions{
			DaemonManager: dm,
			ClientFactory: func(_ context.Context, _, _ string) (*client.Client, error) {
				return nil, clientErr
			},
		})
		defer func() { Configure(oldOpts) }()

		_, err := preRunSetup(ctx, baseURL, token, false)
		if err == nil {
			t.Fatal("expected error from ClientFactory")
		}
	})
}

// mockDaemonManager for unit tests.
type mockDaemonManager struct {
	running     bool
	startCalled bool
}

func (m *mockDaemonManager) IsRunning() bool { return m.running }
func (m *mockDaemonManager) Start() error {
	m.startCalled = true
	return nil
}

// mockAuthnProvider for unit tests.
type mockAuthnProvider struct {
	token string
	err   error
}

func (m *mockAuthnProvider) Authenticate(context.Context) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.token, nil
}

var _ types.DaemonManager = (*mockDaemonManager)(nil)
var _ types.CLIAuthnProvider = (*mockAuthnProvider)(nil)

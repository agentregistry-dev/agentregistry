package openshell

import (
	"strings"
	"testing"
)

func TestKagentNetworkPoliciesForModelProvider(t *testing.T) {
	tests := []struct {
		provider string
		wantKey  string
		wantHost string
		empty    bool
	}{
		{"google", "gemini_api", "generativelanguage.googleapis.com", false},
		{"GEMINI", "gemini_api", "generativelanguage.googleapis.com", false},
		{"anthropic", "anthropic_api", "api.anthropic.com", false},
		{"openai", "openai_api", "api.openai.com", false},
		{"nvidia", "nvidia_api", "integrate.api.nvidia.com", false},
		{"", "gemini_api", "generativelanguage.googleapis.com", false},
		{"unknown-custom", "", "", true},
	}

	for _, tt := range tests {
		name := tt.provider
		if name == "" {
			name = "(empty)"
		}
		t.Run(name, func(t *testing.T) {
			got := kagentNetworkPoliciesForModelProvider(tt.provider)
			if tt.empty {
				if len(got) != 0 {
					t.Fatalf("expected no policies, got %d", len(got))
				}
				return
			}
			rule, ok := got[tt.wantKey]
			if !ok || rule == nil {
				t.Fatalf("missing policy key %q, got %v", tt.wantKey, got)
			}
			if len(rule.GetEndpoints()) == 0 {
				t.Fatal("no endpoints")
			}
			if h := rule.GetEndpoints()[0].GetHost(); h != tt.wantHost {
				t.Errorf("host = %q, want %q", h, tt.wantHost)
			}
			if len(rule.GetBinaries()) == 0 {
				t.Error("expected non-empty binaries allowlist")
			}
		})
	}
}

func TestKagentNetworkPoliciesForModelProvider_TrimsAndLowercases(t *testing.T) {
	got := kagentNetworkPoliciesForModelProvider("  OpenAI ")
	rule := got["openai_api"]
	if rule == nil {
		t.Fatal("expected openai_api")
	}
	if rule.GetEndpoints()[0].GetHost() != "api.openai.com" {
		t.Errorf("host = %q", rule.GetEndpoints()[0].GetHost())
	}
}

func TestKagentADKNetworkBinaryAllowlist_ArchPaths(t *testing.T) {
	bin := kagentADKNetworkBinaryAllowlist()
	if len(bin) < 4 {
		t.Fatalf("want at least 4 binary paths, got %d", len(bin))
	}
	paths := make([]string, len(bin))
	for i, b := range bin {
		paths[i] = b.GetPath()
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "aarch64-gnu") || !strings.Contains(joined, "x86_64-gnu") {
		t.Fatalf("expected aarch64 and x86_64 cpython paths, got %v", paths)
	}
}

func TestKagentRESTEndpoint(t *testing.T) {
	ep := kagentRESTEndpoint("example.com", kagentL7PostV1Star)
	if ep.GetHost() != "example.com" || ep.GetPort() != 443 {
		t.Fatalf("endpoint: %+v", ep)
	}
	if ep.GetProtocol() != "rest" || ep.GetTls() != "terminate" || ep.GetEnforcement() != "enforce" {
		t.Errorf("unexpected TLS/protocol/enforcement: %+v", ep)
	}
	if len(ep.GetRules()) != 1 || ep.GetRules()[0].GetAllow().GetPath() != "/v1/**" {
		t.Errorf("rules: %+v", ep.GetRules())
	}
}

func TestKagentL7GeminiPaths(t *testing.T) {
	if len(kagentL7GeminiPaths) != 2 {
		t.Fatalf("gemini rules count = %d", len(kagentL7GeminiPaths))
	}
	paths := kagentL7GeminiPaths[0].GetAllow().GetPath() + "," + kagentL7GeminiPaths[1].GetAllow().GetPath()
	if !strings.Contains(paths, "/v1beta/") || !strings.Contains(paths, "/v1/") {
		t.Errorf("paths = %s", paths)
	}
}

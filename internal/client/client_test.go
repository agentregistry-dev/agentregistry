package client

import (
	"testing"
)

func TestEnsureV0Suffix(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"bare URL", "http://localhost:12121", "http://localhost:12121/v0"},
		{"already has /v0", "http://localhost:12121/v0", "http://localhost:12121/v0"},
		{"trailing slash", "http://localhost:12121/", "http://localhost:12121/v0"},
		{"trailing slash with v0", "http://localhost:12121/v0/", "http://localhost:12121/v0"},
		{"https URL", "https://registry.example.com", "https://registry.example.com/v0"},
		{"https with v0", "https://registry.example.com/v0", "https://registry.example.com/v0"},
		{"with port", "http://myhost:8080", "http://myhost:8080/v0"},
		{"empty string", "", "/v0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ensureV0Suffix(tt.input)
			if got != tt.want {
				t.Errorf("ensureV0Suffix(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestNewClient_BaseURL(t *testing.T) {
	tests := []struct {
		name    string
		baseURL string
		wantURL string
	}{
		{"empty defaults to defaultBaseURL", "", defaultBaseURL},
		{"bare URL gets /v0 appended", "http://localhost:12121", "http://localhost:12121/v0"},
		{"URL with /v0 unchanged", "http://localhost:12121/v0", "http://localhost:12121/v0"},
		{"trailing slash normalized", "http://localhost:12121/", "http://localhost:12121/v0"},
		{"custom host", "https://registry.example.com", "https://registry.example.com/v0"},
		{"custom host with /v0", "https://registry.example.com/v0", "https://registry.example.com/v0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := NewClient(tt.baseURL, "test-token")
			if c.BaseURL != tt.wantURL {
				t.Errorf("NewClient(%q, ...).BaseURL = %q, want %q", tt.baseURL, c.BaseURL, tt.wantURL)
			}
		})
	}
}

func TestNewClient_Token(t *testing.T) {
	c := NewClient("http://localhost:12121", "my-secret-token")
	if c.token != "my-secret-token" {
		t.Errorf("NewClient token = %q, want %q", c.token, "my-secret-token")
	}

	c2 := NewClient("http://localhost:12121", "")
	if c2.token != "" {
		t.Errorf("NewClient empty token = %q, want empty", c2.token)
	}
}

func TestNewClient_HttpClientNotNil(t *testing.T) {
	c := NewClient("", "")
	if c.httpClient == nil {
		t.Error("NewClient httpClient should not be nil")
	}
}

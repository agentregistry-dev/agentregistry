package client

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
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
		{"empty defaults to DefaultBaseURL", "", DefaultBaseURL},
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

func TestExtractAPIErrorMessage(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{
			"single error message",
			`{"title":"Bad Request","status":400,"detail":"Failed to create server","errors":[{"message":"name is required"}]}`,
			"name is required",
		},
		{
			"multiple error messages",
			`{"title":"Bad Request","status":400,"detail":"Validation failed","errors":[{"message":"name is required"},{"message":"version is invalid"}]}`,
			"name is required; version is invalid",
		},
		{
			"falls back to detail when no error messages",
			`{"title":"Bad Request","status":400,"detail":"Something went wrong","errors":[]}`,
			"Something went wrong",
		},
		{
			"detail only no errors field",
			`{"title":"Internal Server Error","status":500,"detail":"Unexpected failure"}`,
			"Unexpected failure",
		},
		{
			"skips empty messages",
			`{"title":"Bad Request","status":400,"detail":"fail","errors":[{"message":""},{"message":"real error"}]}`,
			"real error",
		},
		{
			"invalid JSON returns empty",
			`not json at all`,
			"",
		},
		{
			"empty body returns empty",
			``,
			"",
		},
		{
			"no detail or errors returns empty",
			`{"title":"Bad Request","status":400}`,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractAPIErrorMessage([]byte(tt.body))
			if got != tt.want {
				t.Errorf("extractAPIErrorMessage() = %q, want %q", got, tt.want)
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

func TestDoJSON_NotFoundKeepsServerDetail(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantMsg string
	}{
		{name: "detail", body: `{"detail":"\"all\" is read-only; select one namespace"}`, wantMsg: `resource not found: "all" is read-only; select one namespace`},
		{name: "no body", wantMsg: "resource not found"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tt.body))
			}))
			t.Cleanup(srv.Close)

			_, err := NewClient(srv.URL, "").GetLatest(context.Background(), "Agent", "team-a", "acme")
			if !errors.Is(err, ErrNotFound) {
				t.Fatalf("GetLatest() error = %v, want ErrNotFound", err)
			}
			if err.Error() != tt.wantMsg {
				t.Fatalf("GetLatest() error = %q, want %q", err.Error(), tt.wantMsg)
			}
		})
	}
}

func TestApplyBatch_SendsNamespace(t *testing.T) {
	tests := []struct {
		name      string
		opts      ApplyOpts
		del       bool
		wantQuery string
	}{
		{name: "no options", wantQuery: ""},
		{name: "namespace", opts: ApplyOpts{Namespace: "team-a"}, wantQuery: "namespace=team-a"},
		{name: "dry run and namespace", opts: ApplyOpts{DryRun: true, Namespace: "team-a"}, wantQuery: "dryRun=true&namespace=team-a"},
		{name: "delete with namespace", opts: ApplyOpts{Namespace: "team-a"}, del: true, wantQuery: "namespace=team-a"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotMethod, gotQuery string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotMethod, gotQuery = r.Method, r.URL.RawQuery
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"results":[]}`))
			}))
			t.Cleanup(srv.Close)

			c := NewClient(srv.URL, "")
			var err error
			if tt.del {
				_, err = c.DeleteViaApply(context.Background(), []byte("{}"), tt.opts)
			} else {
				_, err = c.Apply(context.Background(), []byte("{}"), tt.opts)
			}
			if err != nil {
				t.Fatalf("apply error = %v", err)
			}
			wantMethod := http.MethodPost
			if tt.del {
				wantMethod = http.MethodDelete
			}
			if gotMethod != wantMethod || gotQuery != tt.wantQuery {
				t.Fatalf("request = %s ?%s, want %s ?%s", gotMethod, gotQuery, wantMethod, tt.wantQuery)
			}
		})
	}
}

package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRootUsesConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/v0/version" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":"v0.0.0","gitCommit":"test","buildTime":"test"}`))
	}))
	t.Cleanup(srv.Close)

	cfg := DefaultConfig()
	cfg.Env = mapEnv{
		"ARCTL_API_BASE_URL": srv.URL,
	}

	cmd := Root(cfg)
	if cmd == nil {
		t.Fatal("Root returned nil")
	}
	if cmd.Use != "arctl" {
		t.Fatalf("Root.Use = %q, want %q", cmd.Use, "arctl")
	}

	if cmd.PersistentFlags().Lookup("registry-url") == nil {
		t.Fatal("expected registry-url flag")
	}
	if cmd.PersistentFlags().Lookup("registry-token") == nil {
		t.Fatal("expected registry-token flag")
	}

	cmd.SetArgs([]string{"version"})
	err := cmd.Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
}

type mapEnv map[string]string

func (e mapEnv) Getenv(key string) string {
	return e[key]
}

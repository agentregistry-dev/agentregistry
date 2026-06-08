package common

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

func TestListDeploymentsDefaultsOriginManaged(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if r.URL.Path != "/v0/deployments" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	}))
	t.Cleanup(srv.Close)

	_, err := ListDeployments(context.Background(), client.NewClient(srv.URL, ""))
	if err != nil {
		t.Fatalf("ListDeployments() error = %v", err)
	}

	query, err := url.ParseQuery(rawQuery)
	if err != nil {
		t.Fatalf("ParseQuery(%q) error = %v", rawQuery, err)
	}
	if got := query.Get("origin"); got != deploymentOriginManaged {
		t.Fatalf("origin query = %q, want %q", got, deploymentOriginManaged)
	}
}

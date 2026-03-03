package prompt

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
)

func TestRunDelete_NilClient(t *testing.T) {
	oldClient := apiClient
	apiClient = nil
	defer func() { apiClient = oldClient }()

	deleteVersion = "1.0.0"
	defer func() { deleteVersion = "" }()

	err := runDelete(DeleteCmd, []string{"some-prompt"})
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	if err.Error() != "API client not initialized" {
		t.Errorf("expected 'API client not initialized', got %q", err.Error())
	}
}

func TestRunDelete_Success(t *testing.T) {
	var deletedPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete && strings.Contains(r.URL.Path, "/prompts/") {
			deletedPath = r.URL.Path
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	deleteVersion = "1.0.0"
	defer func() { deleteVersion = "" }()

	err := runDelete(DeleteCmd, []string{"my-prompt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(deletedPath, "my-prompt") {
		t.Errorf("expected delete path to contain 'my-prompt', got %q", deletedPath)
	}
	if !strings.Contains(deletedPath, "1.0.0") {
		t.Errorf("expected delete path to contain '1.0.0', got %q", deletedPath)
	}
}

func TestRunDelete_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	deleteVersion = "1.0.0"
	defer func() { deleteVersion = "" }()

	err := runDelete(DeleteCmd, []string{"fail-prompt"})
	if err == nil {
		t.Fatal("expected error from API failure, got nil")
	}
}

func TestRunDelete_NameAndVersionInPath(t *testing.T) {
	tests := []struct {
		name        string
		promptName  string
		version     string
		wantPathHas []string
	}{
		{
			name:        "simple name",
			promptName:  "my-prompt",
			version:     "1.0.0",
			wantPathHas: []string{"my-prompt", "1.0.0"},
		},
		{
			name:        "different version",
			promptName:  "system-prompt",
			version:     "2.5.1",
			wantPathHas: []string{"system-prompt", "2.5.1"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedPath string
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedPath = r.URL.Path
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			oldClient := apiClient
			apiClient = client.NewClient(ts.URL, "")
			defer func() { apiClient = oldClient }()

			deleteVersion = tt.version
			defer func() { deleteVersion = "" }()

			err := runDelete(DeleteCmd, []string{tt.promptName})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			for _, sub := range tt.wantPathHas {
				if !strings.Contains(capturedPath, sub) {
					t.Errorf("expected path to contain %q, got %q", sub, capturedPath)
				}
			}
		})
	}
}

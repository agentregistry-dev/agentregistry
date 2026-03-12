package skill

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// newTestServer creates an httptest server that serves skill API responses.
// The handler map keys are URL paths (without the /v0 prefix), values are the
// handlers to invoke. The /v0 prefix is prepended automatically to match the
// client which appends /v0 to the base URL.
func newTestServer(t *testing.T, handlers map[string]http.HandlerFunc) (*httptest.Server, *client.Client) {
	t.Helper()
	mux := http.NewServeMux()
	for pattern, handler := range handlers {
		mux.HandleFunc("/v0"+pattern, handler)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := client.NewClient(srv.URL, "")
	return srv, c
}

func jsonResponse(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Fatalf("failed to encode JSON response: %v", err)
	}
}

func TestResolveSkillVersion(t *testing.T) {
	t.Run("explicit version returns immediately", func(t *testing.T) {
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		// apiClient doesn't need to be set when version is explicit
		apiClient = nil

		v, err := resolveSkillVersion("my-skill", "1.0.0")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "1.0.0" {
			t.Errorf("version = %q, want %q", v, "1.0.0")
		}
	})

	t.Run("single version auto-selected", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/my-skill/versions": func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, models.SkillListResponse{
					Skills: []models.SkillResponse{
						{Skill: models.SkillJSON{Name: "my-skill", Version: "2.0.0"}},
					},
				})
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		v, err := resolveSkillVersion("my-skill", "")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if v != "2.0.0" {
			t.Errorf("version = %q, want %q", v, "2.0.0")
		}
	})

	t.Run("multiple versions requires explicit selection", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/my-skill/versions": func(w http.ResponseWriter, r *http.Request) {
				jsonResponse(t, w, models.SkillListResponse{
					Skills: []models.SkillResponse{
						{Skill: models.SkillJSON{Name: "my-skill", Version: "1.0.0"}},
						{Skill: models.SkillJSON{Name: "my-skill", Version: "2.0.0"}},
					},
				})
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("my-skill", "")
		if err == nil {
			t.Fatal("expected error for multiple versions, got nil")
		}
		if got := err.Error(); !stringContains(got, "multiple versions") {
			t.Errorf("error = %q, want it to contain 'multiple versions'", got)
		}
	})

	t.Run("no versions found", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/unknown/versions": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusNotFound)
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("unknown", "")
		if err == nil {
			t.Fatal("expected error for unknown skill, got nil")
		}
		if got := err.Error(); !stringContains(got, "not found") {
			t.Errorf("error = %q, want it to contain 'not found'", got)
		}
	})

	t.Run("API error propagated", func(t *testing.T) {
		_, c := newTestServer(t, map[string]http.HandlerFunc{
			"/skills/broken/versions": func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
			},
		})
		origClient := apiClient
		t.Cleanup(func() { apiClient = origClient })
		apiClient = c

		_, err := resolveSkillVersion("broken", "")
		if err == nil {
			t.Fatal("expected error for API failure, got nil")
		}
	})
}

func TestRunPull_NilClient(t *testing.T) {
	origClient := apiClient
	t.Cleanup(func() { apiClient = origClient })
	apiClient = nil

	err := runPull(nil, []string{"some-skill"})
	if err == nil {
		t.Fatal("expected error for nil apiClient, got nil")
	}
	if got := err.Error(); !stringContains(got, "API client not initialized") {
		t.Errorf("error = %q, want it to contain 'API client not initialized'", got)
	}
}

func TestRunPull_SkillNotFound(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/nonexistent/versions": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	origClient := apiClient
	t.Cleanup(func() { apiClient = origClient })
	apiClient = c

	err := runPull(nil, []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected error for missing skill, got nil")
	}
}

func TestRunPull_NoSourceAvailable(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/no-source/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "no-source", Version: "1.0.0"}},
				},
			})
		},
		"/skills/no-source/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "no-source",
					Version: "1.0.0",
					// No packages, no repository
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
	})
	apiClient = c
	pullVersion = ""

	err := runPull(nil, []string{"no-source"})
	if err == nil {
		t.Fatal("expected error for skill with no source, got nil")
	}
	if got := err.Error(); !stringContains(got, "no Docker package or Git repository") {
		t.Errorf("error = %q, want it to contain 'no Docker package or Git repository'", got)
	}
}

func TestRunPull_OutputDirDefault(t *testing.T) {
	// Verify the default output directory is "skills/<name>"
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/myskill/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "myskill", Version: "1.0.0"}},
				},
			})
		},
		"/skills/myskill/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "myskill",
					Version: "1.0.0",
					// No sources - will fail, but we check the output dir was created
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	origDir, _ := os.Getwd()
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
		os.Chdir(origDir)
	})
	apiClient = c
	pullVersion = ""

	tmpDir := t.TempDir()
	os.Chdir(tmpDir)

	// Will fail because no source, but output dir should be created
	_ = runPull(nil, []string{"myskill"})

	expectedDir := filepath.Join(tmpDir, "skills", "myskill")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Errorf("expected default output directory %s to be created", expectedDir)
	}
}

func TestRunPull_CustomOutputDir(t *testing.T) {
	_, c := newTestServer(t, map[string]http.HandlerFunc{
		"/skills/myskill/versions": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillListResponse{
				Skills: []models.SkillResponse{
					{Skill: models.SkillJSON{Name: "myskill", Version: "1.0.0"}},
				},
			})
		},
		"/skills/myskill/versions/1.0.0": func(w http.ResponseWriter, r *http.Request) {
			jsonResponse(t, w, models.SkillResponse{
				Skill: models.SkillJSON{
					Name:    "myskill",
					Version: "1.0.0",
				},
			})
		},
	})
	origClient := apiClient
	origVersion := pullVersion
	t.Cleanup(func() {
		apiClient = origClient
		pullVersion = origVersion
	})
	apiClient = c
	pullVersion = ""

	tmpDir := t.TempDir()
	customDir := filepath.Join(tmpDir, "my-custom-output")

	// Will fail because no source, but custom output dir should be created
	_ = runPull(nil, []string{"myskill", customDir})

	if _, err := os.Stat(customDir); os.IsNotExist(err) {
		t.Errorf("expected custom output directory %s to be created", customDir)
	}
}

// stringContains is a simple helper to check substring presence.
func stringContains(s, substr string) bool {
	return strings.Contains(s, substr)
}

package prompt

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

func TestReadTextPrompt(t *testing.T) {
	// readTextPrompt reads package-level flag vars, so set them here.
	publishName = "test-prompt"
	publishVersion = "1.0.0"
	publishDescription = "A test prompt"
	defer func() {
		publishName = ""
		publishVersion = ""
		publishDescription = ""
	}()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	content := "You are a helpful assistant."
	if err := os.WriteFile(filePath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p, err := readTextPrompt(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "test-prompt" {
		t.Errorf("expected name %q, got %q", "test-prompt", p.Name)
	}
	if p.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", p.Version)
	}
	if p.Description != "A test prompt" {
		t.Errorf("expected description %q, got %q", "A test prompt", p.Description)
	}
	if p.Content != content {
		t.Errorf("expected content %q, got %q", content, p.Content)
	}
}

func TestReadTextPrompt_FileNotFound(t *testing.T) {
	_, err := readTextPrompt("/nonexistent/file.txt")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestReadPromptYAML(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.yaml")
	yamlContent := `name: yaml-prompt
version: 2.0.0
description: A YAML prompt
content: You are a coding assistant.
`
	if err := os.WriteFile(filePath, []byte(yamlContent), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	p, err := readPromptYAML(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.Name != "yaml-prompt" {
		t.Errorf("expected name %q, got %q", "yaml-prompt", p.Name)
	}
	if p.Version != "2.0.0" {
		t.Errorf("expected version %q, got %q", "2.0.0", p.Version)
	}
	if p.Description != "A YAML prompt" {
		t.Errorf("expected description %q, got %q", "A YAML prompt", p.Description)
	}
	if p.Content != "You are a coding assistant." {
		t.Errorf("expected content %q, got %q", "You are a coding assistant.", p.Content)
	}
}

func TestReadPromptYAML_FileNotFound(t *testing.T) {
	_, err := readPromptYAML("/nonexistent/prompt.yaml")
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestReadPromptYAML_InvalidYAML(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "bad.yaml")
	// Unterminated flow sequence is genuinely invalid YAML.
	if err := os.WriteFile(filePath, []byte("name: [\n"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	_, err := readPromptYAML(filePath)
	if err == nil {
		t.Fatal("expected error for invalid YAML, got nil")
	}
}

func TestApplyPublishFlags(t *testing.T) {
	tests := []struct {
		name        string
		initial     models.PromptJSON
		flagName    string
		flagVersion string
		flagDesc    string
		wantName    string
		wantVersion string
		wantDesc    string
	}{
		{
			name:        "all flags override",
			initial:     models.PromptJSON{Name: "yaml-name", Version: "1.0.0", Description: "yaml desc"},
			flagName:    "flag-name",
			flagVersion: "2.0.0",
			flagDesc:    "flag desc",
			wantName:    "flag-name",
			wantVersion: "2.0.0",
			wantDesc:    "flag desc",
		},
		{
			name:        "no flags keeps original",
			initial:     models.PromptJSON{Name: "yaml-name", Version: "1.0.0", Description: "yaml desc"},
			flagName:    "",
			flagVersion: "",
			flagDesc:    "",
			wantName:    "yaml-name",
			wantVersion: "1.0.0",
			wantDesc:    "yaml desc",
		},
		{
			name:        "partial flags override",
			initial:     models.PromptJSON{Name: "yaml-name", Version: "1.0.0", Description: "yaml desc"},
			flagName:    "new-name",
			flagVersion: "",
			flagDesc:    "",
			wantName:    "new-name",
			wantVersion: "1.0.0",
			wantDesc:    "yaml desc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set package-level vars
			publishName = tt.flagName
			publishVersion = tt.flagVersion
			publishDescription = tt.flagDesc
			defer func() {
				publishName = ""
				publishVersion = ""
				publishDescription = ""
			}()

			p := tt.initial
			applyPublishFlags(&p)

			if p.Name != tt.wantName {
				t.Errorf("Name: got %q, want %q", p.Name, tt.wantName)
			}
			if p.Version != tt.wantVersion {
				t.Errorf("Version: got %q, want %q", p.Version, tt.wantVersion)
			}
			if p.Description != tt.wantDesc {
				t.Errorf("Description: got %q, want %q", p.Description, tt.wantDesc)
			}
		})
	}
}

func TestRunPublish_NilClient(t *testing.T) {
	oldClient := apiClient
	apiClient = nil
	defer func() { apiClient = oldClient }()

	err := runPublish(PublishCmd, []string{"somefile.txt"})
	if err == nil {
		t.Fatal("expected error for nil client, got nil")
	}
	if err.Error() != "API client not initialized" {
		t.Errorf("expected 'API client not initialized', got %q", err.Error())
	}
}

func TestRunPublish_FileNotExist(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	err := runPublish(PublishCmd, []string{"/nonexistent/file.txt"})
	if err == nil {
		t.Fatal("expected error for nonexistent file, got nil")
	}
}

func TestRunPublish_Directory(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	dir := t.TempDir()
	err := runPublish(PublishCmd, []string{dir})
	if err == nil {
		t.Fatal("expected error for directory path, got nil")
	}
}

func TestRunPublish_MissingName(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	publishName = ""
	publishVersion = "1.0.0"
	defer func() { publishName = ""; publishVersion = "" }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	os.WriteFile(filePath, []byte("content"), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err == nil {
		t.Fatal("expected error for missing name, got nil")
	}
	if err.Error() != "prompt name is required (use --name flag)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPublish_MissingVersion(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	publishName = "test-prompt"
	publishVersion = ""
	defer func() { publishName = ""; publishVersion = "" }()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	os.WriteFile(filePath, []byte("content"), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
	if err.Error() != "prompt version is required (use --version flag)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestRunPublish_TextFile_Success(t *testing.T) {
	var receivedPrompt models.PromptJSON
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/prompts" {
			json.NewDecoder(r.Body).Decode(&receivedPrompt)
			resp := models.PromptResponse{
				Prompt: receivedPrompt,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	publishName = "my-prompt"
	publishVersion = "1.0.0"
	publishDescription = "My test prompt"
	dryRunFlag = false
	defer func() {
		publishName = ""
		publishVersion = ""
		publishDescription = ""
		dryRunFlag = false
	}()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	os.WriteFile(filePath, []byte("You are helpful."), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedPrompt.Name != "my-prompt" {
		t.Errorf("expected name %q, got %q", "my-prompt", receivedPrompt.Name)
	}
	if receivedPrompt.Version != "1.0.0" {
		t.Errorf("expected version %q, got %q", "1.0.0", receivedPrompt.Version)
	}
	if receivedPrompt.Content != "You are helpful." {
		t.Errorf("expected content %q, got %q", "You are helpful.", receivedPrompt.Content)
	}
}

func TestRunPublish_YAMLFile_Success(t *testing.T) {
	var receivedPrompt models.PromptJSON
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/v0/prompts" {
			json.NewDecoder(r.Body).Decode(&receivedPrompt)
			resp := models.PromptResponse{
				Prompt: receivedPrompt,
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	// Clear flags so YAML values are used
	publishName = ""
	publishVersion = ""
	publishDescription = ""
	dryRunFlag = false
	defer func() {
		publishName = ""
		publishVersion = ""
		publishDescription = ""
		dryRunFlag = false
	}()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.yaml")
	yamlContent := `name: yaml-prompt
version: 2.0.0
description: From YAML
content: YAML content here
`
	os.WriteFile(filePath, []byte(yamlContent), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if receivedPrompt.Name != "yaml-prompt" {
		t.Errorf("expected name %q, got %q", "yaml-prompt", receivedPrompt.Name)
	}
	if receivedPrompt.Version != "2.0.0" {
		t.Errorf("expected version %q, got %q", "2.0.0", receivedPrompt.Version)
	}
}

func TestRunPublish_DryRun(t *testing.T) {
	serverCalled := false
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			serverCalled = true
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	publishName = "dry-prompt"
	publishVersion = "1.0.0"
	dryRunFlag = true
	defer func() {
		publishName = ""
		publishVersion = ""
		dryRunFlag = false
	}()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	os.WriteFile(filePath, []byte("dry run content"), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if serverCalled {
		t.Error("expected server NOT to be called during dry run")
	}
}

func TestRunPublish_APIError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	oldClient := apiClient
	apiClient = client.NewClient(ts.URL, "")
	defer func() { apiClient = oldClient }()

	publishName = "fail-prompt"
	publishVersion = "1.0.0"
	dryRunFlag = false
	defer func() {
		publishName = ""
		publishVersion = ""
		dryRunFlag = false
	}()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "prompt.txt")
	os.WriteFile(filePath, []byte("content"), 0644)

	err := runPublish(PublishCmd, []string{filePath})
	if err == nil {
		t.Fatal("expected error from API failure, got nil")
	}
}

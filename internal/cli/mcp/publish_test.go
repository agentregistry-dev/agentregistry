package mcp

import (
	"fmt"
	"testing"
)

func TestResolveTransport(t *testing.T) {
	tests := []struct {
		name                  string
		transportType         string
		transportURL          string
		expectedTransportType string
		expectedTransportURL  string
		expectingError        bool
	}{
		{
			name:                  "stdio transport",
			transportType:         "stdio",
			transportURL:          "",
			expectedTransportType: "stdio",
			expectedTransportURL:  "",
			expectingError:        false,
		},
		{
			name:                  "invalid transport",
			transportType:         "http",
			transportURL:          "http://localhost:8080",
			expectedTransportType: "http",
			expectedTransportURL:  "http://localhost:8080",
			expectingError:        true,
		},
		{
			name:                  "streamable-http with URL",
			transportType:         "streamable-http",
			transportURL:          "http://localhost:8080",
			expectedTransportType: "streamable-http",
			expectedTransportURL:  "http://localhost:8080",
			expectingError:        false,
		},
		{
			name:                  "streamable-http without URL defaults to localhost",
			transportType:         "streamable-http",
			transportURL:          "",
			expectedTransportType: "streamable-http",
			expectedTransportURL:  "http://localhost:3000/mcp",
			expectingError:        false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			transport, url, err := resolveTransport(tt.transportType, tt.transportURL)
			if tt.expectingError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if transport != tt.expectedTransportType {
				t.Errorf("expected %s but got %s", tt.expectedTransportType, transport)
			}
			if url != tt.expectedTransportURL {
				t.Errorf("expected %s but got %s", tt.expectedTransportURL, url)
			}
		})
	}
}

func TestValidateRegistryType(t *testing.T) {
	tests := []struct {
		registryType   string
		expectingError bool
		expected       string
	}{
		{
			registryType:   "npm",
			expectingError: false,
			expected:       "npm",
		},
		{
			registryType:   "NPM",
			expectingError: false,
			expected:       "npm",
		},
		{
			registryType:   "oci",
			expectingError: false,
			expected:       "oci",
		},
		{
			registryType:   "OCI",
			expectingError: false,
			expected:       "oci",
		},
		{
			registryType:   "pypi",
			expectingError: false,
			expected:       "pypi",
		},
		{
			registryType:   "blah",
			expectingError: true,
			expected:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.registryType, func(t *testing.T) {
			actual, err := validateRegistryType(tt.registryType)
			if tt.expectingError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if actual != tt.expected {
					t.Errorf("expected %s but got %s", tt.expected, actual)
				}
			}
		})
	}
}

func TestBuildRepository(t *testing.T) {
	tests := []struct {
		name      string
		gitURL string
		expectNil bool
	}{
		{
			name:      "empty URL returns nil",
			gitURL: "",
			expectNil: true,
		},
		{
			name:      "valid URL returns repository",
			gitURL: "https://github.com/user/repo",
			expectNil: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildRepository(tt.gitURL)
			if tt.expectNil {
				if result != nil {
					t.Errorf("expected nil but got %v", result)
				}
				return
			}
			if result == nil { //nolint:staticcheck
				t.Error("expected non-nil result")
			}
			if result.URL != tt.gitURL { //nolint:staticcheck
				t.Errorf("expected URL %s but got %s", tt.gitURL, result.URL)
			}
			if result.Source != "git" {
				t.Errorf("expected source 'git' but got %s", result.Source)
			}
		})
	}
}

func TestBuildArguments(t *testing.T) {
	tests := []struct {
		name     string
		args     []string
		expected int
	}{
		{
			name:     "nil args returns nil",
			args:     nil,
			expected: 0,
		},
		{
			name:     "empty args returns nil",
			args:     []string{},
			expected: 0,
		},
		{
			name:     "single arg",
			args:     []string{"/path/to/dir"},
			expected: 1,
		},
		{
			name:     "multiple args",
			args:     []string{"/path/one", "/path/two", "/path/three"},
			expected: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildArguments(tt.args)
			if tt.expected == 0 {
				if result != nil {
					t.Errorf("expected nil but got %v", result)
				}
			} else {
				if len(result) != tt.expected {
					t.Errorf("expected %d arguments but got %d", tt.expected, len(result))
				}
				for i, arg := range result {
					if arg.Value != tt.args[i] {
						t.Errorf("expected value %s but got %s", tt.args[i], arg.Value)
					}
				}
			}
		})
	}
}

func TestBuildServerJSON(t *testing.T) {
	params := ServerJSONParams{
		Name:             "myorg/my-server",
		Description:      "Test server",
		Title:            "My Server",
		Version:          "1.0.0",
		GitURL:        "https://github.com/myorg/my-server",
		RegistryType:     "oci",
		Identifier:       "docker.io/myorg/my-server:1.0.0",
		PackageVersion:   "1.0.0",
		RuntimeHint:      "",
		PackageArguments: []string{"/data"},
		TransportType:    "stdio",
		TransportURL:     "",
	}

	result := buildServerJSON(params)

	if result.Name != params.Name {
		t.Errorf("expected Name %s but got %s", params.Name, result.Name)
	}
	if result.Description != params.Description {
		t.Errorf("expected Description %s but got %s", params.Description, result.Description)
	}
	if result.Version != params.Version {
		t.Errorf("expected Version %s but got %s", params.Version, result.Version)
	}
	if result.Repository == nil {
		t.Error("expected Repository to be set")
	}
	if result.Repository.URL != params.GitURL {
		t.Errorf("expected Repository URL %s but got %s", params.GitURL, result.Repository.URL)
	}
	if len(result.Packages) != 1 {
		t.Fatalf("expected 1 package but got %d", len(result.Packages))
	}

	pkg := result.Packages[0]
	if pkg.RegistryType != params.RegistryType {
		t.Errorf("expected RegistryType %s but got %s", params.RegistryType, pkg.RegistryType)
	}
	if pkg.Identifier != params.Identifier {
		t.Errorf("expected Identifier %s but got %s", params.Identifier, pkg.Identifier)
	}
	if pkg.Transport.Type != params.TransportType {
		t.Errorf("expected Transport.Type %s but got %s", params.TransportType, pkg.Transport.Type)
	}
	if len(pkg.PackageArguments) != 1 {
		t.Errorf("expected 1 PackageArgument but got %d", len(pkg.PackageArguments))
	}
}

func TestBuildRemoteServerJSON(t *testing.T) {
	params := ServerJSONParams{
		Name:          "com.example/my-server",
		Description:   "A remote server",
		Title:         "My Remote Server",
		Version:       "2.0.0",
		GitURL:     "https://github.com/example/my-server",
		TransportType: "streamable-http",
		TransportURL:  "https://api.example.com/mcp",
	}

	result := buildRemoteServerJSON(params)

	if result.Name != params.Name {
		t.Errorf("expected Name %s but got %s", params.Name, result.Name)
	}
	if result.Description != params.Description {
		t.Errorf("expected Description %s but got %s", params.Description, result.Description)
	}
	if result.Version != params.Version {
		t.Errorf("expected Version %s but got %s", params.Version, result.Version)
	}
	if result.Repository == nil {
		t.Fatal("expected Repository to be set")
	}
	if result.Repository.URL != params.GitURL {
		t.Errorf("expected Repository URL %s but got %s", params.GitURL, result.Repository.URL)
	}
	if len(result.Packages) != 0 {
		t.Errorf("expected no packages but got %d", len(result.Packages))
	}
	if len(result.Remotes) != 1 {
		t.Fatalf("expected 1 remote but got %d", len(result.Remotes))
	}
	if result.Remotes[0].Type != params.TransportType {
		t.Errorf("expected remote Type %s but got %s", params.TransportType, result.Remotes[0].Type)
	}
	if result.Remotes[0].URL != params.TransportURL {
		t.Errorf("expected remote URL %s but got %s", params.TransportURL, result.Remotes[0].URL)
	}
}

func TestBuildRemoteServerJSON_NoGithub(t *testing.T) {
	params := ServerJSONParams{
		Name:          "com.example/server",
		Description:   "No github",
		Version:       "1.0.0",
		TransportType: "sse",
		TransportURL:  "https://api.example.com/sse",
	}

	result := buildRemoteServerJSON(params)

	if result.Repository != nil {
		t.Errorf("expected nil Repository but got %v", result.Repository)
	}
	if len(result.Remotes) != 1 {
		t.Fatalf("expected 1 remote but got %d", len(result.Remotes))
	}
	if result.Remotes[0].Type != "sse" {
		t.Errorf("expected remote Type 'sse' but got %s", result.Remotes[0].Type)
	}
}

func TestResolveRemoteTransport(t *testing.T) {
	tests := []struct {
		name           string
		transport      string
		expectedType   string
		expectingError bool
	}{
		{
			name:           "defaults to streamable-http when empty",
			transport:      "",
			expectedType:   "streamable-http",
			expectingError: false,
		},
		{
			name:           "accepts streamable-http",
			transport:      "streamable-http",
			expectedType:   "streamable-http",
			expectingError: false,
		},
		{
			name:           "accepts sse",
			transport:      "sse",
			expectedType:   "sse",
			expectingError: false,
		},
		{
			name:           "rejects stdio",
			transport:      "stdio",
			expectingError: true,
		},
		{
			name:           "rejects invalid value",
			transport:      "http",
			expectingError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Replicate the remote transport validation logic from runMCPServerPublish
			remoteTransportType := tt.transport
			if remoteTransportType == "" {
				remoteTransportType = "streamable-http"
			}
			err := func() error {
				if remoteTransportType != "streamable-http" && remoteTransportType != "sse" {
					return fmt.Errorf("--transport must be 'streamable-http' or 'sse' when using --remote-url (got: %s)", remoteTransportType)
				}
				return nil
			}()

			if tt.expectingError {
				if err == nil {
					t.Errorf("expected error but got none")
				}
				return
			}
			if err != nil {
				t.Errorf("unexpected error: %v", err)
			}
			if remoteTransportType != tt.expectedType {
				t.Errorf("expected transport type %s but got %s", tt.expectedType, remoteTransportType)
			}
		})
	}
}

func TestRegistryTypeRuntimeHints(t *testing.T) {
	tests := []struct {
		regType  string
		expected string
	}{
		{"npm", "npx"},
		{"pypi", "uvx"},
		{"oci", ""},
	}

	for _, tt := range tests {
		t.Run(tt.regType, func(t *testing.T) {
			hint := registryTypeRuntimeHints[tt.regType]
			if hint != tt.expected {
				t.Errorf("expected runtime hint %q for %s but got %q", tt.expected, tt.regType, hint)
			}
		})
	}
}

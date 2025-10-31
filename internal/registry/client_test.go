package registry

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewClient(t *testing.T) {
	client := NewClient()

	if client == nil {
		t.Fatal("NewClient() returned nil")
	}

	if client.HTTPClient == nil {
		t.Fatal("HTTPClient is nil")
	}

	if client.HTTPClient.Timeout == 0 {
		t.Error("HTTPClient timeout not set")
	}
}

func TestValidateRegistry_Success(t *testing.T) {
	// Create a test server that returns a valid registry response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("limit") != "1" {
			t.Errorf("Expected limit=1, got %s", r.URL.Query().Get("limit"))
		}

		response := RegistryResponse{
			Servers: []ServerEntry{
				{
					Server: ServerSpec{
						Name:        "io.test/server",
						Description: "Test server",
						Version:     "1.0.0",
					},
				},
			},
			Metadata: RegistryMetadata{
				Count:      1,
				NextCursor: "",
			},
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient()
	err := client.ValidateRegistry(server.URL)

	if err != nil {
		t.Errorf("ValidateRegistry() failed: %v", err)
	}
}

func TestValidateRegistry_InvalidJSON(t *testing.T) {
	// Create a test server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient()
	err := client.ValidateRegistry(server.URL)

	if err == nil {
		t.Error("ValidateRegistry() should fail with invalid JSON")
	}
}

func TestValidateRegistry_NonOKStatus(t *testing.T) {
	testCases := []struct {
		name       string
		statusCode int
	}{
		{"NotFound", http.StatusNotFound},
		{"InternalError", http.StatusInternalServerError},
		{"Unauthorized", http.StatusUnauthorized},
		{"BadRequest", http.StatusBadRequest},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.statusCode)
			}))
			defer server.Close()

			client := NewClient()
			err := client.ValidateRegistry(server.URL)

			if err == nil {
				t.Errorf("ValidateRegistry() should fail with status code %d", tc.statusCode)
			}
		})
	}
}

func TestValidateRegistry_InvalidURL(t *testing.T) {
	client := NewClient()
	err := client.ValidateRegistry("http://invalid-url-that-does-not-exist-12345.com")

	if err == nil {
		t.Error("ValidateRegistry() should fail with invalid URL")
	}
}

func TestFetchAllServers_SinglePage(t *testing.T) {
	// Create a test server with a single page of results
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RegistryResponse{
			Servers: []ServerEntry{
				{
					Server: ServerSpec{
						Name:        "io.test/server1",
						Description: "Test server 1",
						Version:     "1.0.0",
						Status:      "active",
					},
					Meta: json.RawMessage(`{"official":{"status":"active"}}`),
				},
				{
					Server: ServerSpec{
						Name:        "io.test/server2",
						Description: "Test server 2",
						Version:     "2.0.0",
						Status:      "active",
					},
				Meta: json.RawMessage(`{"official":{"status":"active"}}`),
			},
		},
		Metadata: RegistryMetadata{
			Count:      2,
			NextCursor: "",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
servers, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: false,
		Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}

	if len(servers) != 2 {
		t.Errorf("Expected 2 servers, got %d", len(servers))
	}

	if servers[0].Server.Name != "io.test/server1" {
		t.Errorf("Expected first server name 'io.test/server1', got '%s'", servers[0].Server.Name)
	}
}

func TestFetchAllServers_MultiplePages(t *testing.T) {
	pageNumber := 0

	// Create a test server with multiple pages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")

		var response RegistryResponse

		switch cursor {
		case "":
			// First page
			response = RegistryResponse{
				Servers: []ServerEntry{
					{
						Server: ServerSpec{
							Name:    "io.test/server1",
							Version: "1.0.0",
							Status:  "active",
						},
						Meta: json.RawMessage(`{}`),
					},
				},
				Metadata: RegistryMetadata{
					Count:      1,
					NextCursor: "page2",
				},
			}
			pageNumber = 1
		case "page2":
			// Second page
			response = RegistryResponse{
				Servers: []ServerEntry{
					{
						Server: ServerSpec{
							Name:    "io.test/server2",
							Version: "2.0.0",
							Status:  "active",
						},
						Meta: json.RawMessage(`{}`),
					},
				},
				Metadata: RegistryMetadata{
					Count:      1,
					NextCursor: "page3",
				},
			}
			pageNumber = 2
		case "page3":
			// Third page (last)
			response = RegistryResponse{
				Servers: []ServerEntry{
					{
						Server: ServerSpec{
							Name:    "io.test/server3",
							Version: "3.0.0",
							Status:  "active",
						},
						Meta: json.RawMessage(`{}`),
					},
				},
				Metadata: RegistryMetadata{
					Count:      1,
					NextCursor: "",
				},
			}
			pageNumber = 3
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	client := NewClient()
	servers, err := client.FetchAllServers(server.URL, FetchOptions{
		ShowProgress: false,
		Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}

	if len(servers) != 3 {
		t.Errorf("Expected 3 servers across all pages, got %d", len(servers))
	}

	if pageNumber != 3 {
		t.Errorf("Expected to fetch 3 pages, but only fetched %d", pageNumber)
	}
}

func TestFetchAllServers_FilterInactiveServers(t *testing.T) {
	// Create a test server with mixed active and inactive servers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RegistryResponse{
			Servers: []ServerEntry{
				{
					Server: ServerSpec{
						Name:    "io.test/active1",
						Version: "1.0.0",
						Status:  "active",
					},
					Meta: json.RawMessage(`{}`),
				},
				{
					Server: ServerSpec{
						Name:    "io.test/inactive",
						Version: "1.0.0",
						Status:  "deprecated",
					},
					Meta: json.RawMessage(`{}`),
				},
				{
					Server: ServerSpec{
						Name:    "io.test/active2",
						Version: "2.0.0",
						Status:  "active",
					},
					Meta: json.RawMessage(`{}`),
				},
				{
					Server: ServerSpec{
						Name:    "io.test/archived",
						Version: "1.0.0",
						Status:  "archived",
					},
					Meta: json.RawMessage(`{}`),
				},
			},
		Metadata: RegistryMetadata{
			Count:      4,
			NextCursor: "",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
servers, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: false,
	Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}

	// Should only include active servers
	if len(servers) != 2 {
		t.Errorf("Expected 2 active servers, got %d", len(servers))
	}

	for _, server := range servers {
		if server.Server.Status != "active" && server.Server.Status != "" {
			t.Errorf("Expected only active servers, got status: %s", server.Server.Status)
		}
	}
}

func TestFetchAllServers_EmptyStatus(t *testing.T) {
	// Servers with empty status should be treated as active
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RegistryResponse{
			Servers: []ServerEntry{
				{
					Server: ServerSpec{
						Name:    "io.test/no-status",
						Version: "1.0.0",
						Status:  "", // Empty status
					},
					Meta: json.RawMessage(`{}`),
				},
			},
		Metadata: RegistryMetadata{
			Count:      1,
			NextCursor: "",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
servers, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: false,
	Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}

	if len(servers) != 1 {
		t.Errorf("Expected 1 server with empty status to be included, got %d", len(servers))
	}
}

func TestFetchAllServers_HTTPError(t *testing.T) {
	// Create a test server that returns an error on the second page
	pageCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pageCount++

		if pageCount == 1 {
			// First page succeeds
			response := RegistryResponse{
				Servers: []ServerEntry{
					{
						Server: ServerSpec{
							Name:    "io.test/server1",
							Version: "1.0.0",
							Status:  "active",
						},
						Meta: json.RawMessage(`{}`),
					},
			},
			Metadata: RegistryMetadata{
				Count:      1,
				NextCursor: "page2",
			},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(response)
	} else {
		// Second page fails
		w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	client := NewClient()
	_, err := client.FetchAllServers(server.URL, FetchOptions{
		ShowProgress: false,
		Verbose:      false,
	})

	if err == nil {
		t.Error("FetchAllServers() should fail when server returns error")
	}
}

func TestFetchAllServers_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	client := NewClient()
	_, err := client.FetchAllServers(server.URL, FetchOptions{
		ShowProgress: false,
		Verbose:      false,
	})

	if err == nil {
		t.Error("FetchAllServers() should fail with invalid JSON")
	}
}

func TestFetchAllServers_WithProgressBar(t *testing.T) {
	// Test that progress bar doesn't cause errors
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RegistryResponse{
			Servers: []ServerEntry{
				{
					Server: ServerSpec{
						Name:    "io.test/server1",
						Version: "1.0.0",
						Status:  "active",
					},
					Meta: json.RawMessage(`{}`),
				},
			},
		Metadata: RegistryMetadata{
			Count:      1,
			NextCursor: "",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
servers, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: true,
	Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() with progress bar failed: %v", err)
	}

	if len(servers) != 1 {
		t.Errorf("Expected 1 server, got %d", len(servers))
	}
}

func TestFetchAllServers_EmptyRegistry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		response := RegistryResponse{
			Servers: []ServerEntry{},
		Metadata: RegistryMetadata{
			Count:      0,
			NextCursor: "",
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
servers, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: false,
	Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}

	if len(servers) != 0 {
		t.Errorf("Expected 0 servers from empty registry, got %d", len(servers))
	}
}

func TestFetchAllServers_PaginationLimit(t *testing.T) {
	// Test that the correct limit parameter is sent
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		limit := r.URL.Query().Get("limit")
		if limit != "100" {
			t.Errorf("Expected limit=100, got %s", limit)
		}

	response := RegistryResponse{
		Servers:  []ServerEntry{},
		Metadata: RegistryMetadata{Count: 0, NextCursor: ""},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(response)
}))
defer server.Close()

client := NewClient()
_, err := client.FetchAllServers(server.URL, FetchOptions{
	ShowProgress: false,
	Verbose:      false,
	})

	if err != nil {
		t.Fatalf("FetchAllServers() failed: %v", err)
	}
}

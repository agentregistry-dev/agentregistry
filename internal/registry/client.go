package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/types"
	"github.com/schollz/progressbar/v3"
)

// Client handles communication with registries
type Client struct {
	HTTPClient *http.Client
}

// NewClient creates a new registry client
func NewClient() *Client {
	return &Client{
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ValidateRegistry checks if the URL hosts a valid registry
func (c *Client) ValidateRegistry(baseURL string) error {
	// Try to fetch the first page with limit=1 to validate
	testURL := fmt.Sprintf("%s?limit=1", baseURL)

	resp, err := c.HTTPClient.Get(testURL)
	if err != nil {
		return fmt.Errorf("failed to connect to registry: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("registry returned status %d (expected 200)", resp.StatusCode)
	}

	// Try to parse the response to validate it's a proper registry
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read registry response: %w", err)
	}

	var registryResp types.RegistryResponse
	if err := json.Unmarshal(body, &registryResp); err != nil {
		return fmt.Errorf("invalid registry format: %w", err)
	}

	return nil
}

// FetchOptions configures the fetch behavior
type FetchOptions struct {
	ShowProgress bool
	Verbose      bool
}

// FetchAllServers fetches all servers from a registry with pagination
func (c *Client) FetchAllServers(baseURL string, opts FetchOptions) ([]types.ServerEntry, error) {
	var allServers []types.ServerEntry
	cursor := ""
	pageCount := 0
	const pageLimit = 100

	// Construct the endpoint: /v0/servers
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v0/servers") {
		baseURL = baseURL + "/v0/servers"
	}

	// First, get the total count estimate for progress bar
	var bar *progressbar.ProgressBar
	if opts.ShowProgress {
		bar = progressbar.NewOptions(-1,
			progressbar.OptionSetDescription("Fetching servers"),
			progressbar.OptionSetWriter(io.Discard), // We'll update manually
			progressbar.OptionShowCount(),
			progressbar.OptionShowIts(),
			progressbar.OptionSetItsString("servers"),
			progressbar.OptionThrottle(65*time.Millisecond),
			progressbar.OptionSpinnerType(14),
			progressbar.OptionFullWidth(),
		)
	}

	// Fetch all pages using cursor-based pagination
	for {
		pageCount++

		// Build URL with pagination parameters
		fetchURL := fmt.Sprintf("%s?limit=%d", baseURL, pageLimit)
		if cursor != "" {
			fetchURL = fmt.Sprintf("%s&cursor=%s", fetchURL, url.QueryEscape(cursor))
		}

		if opts.Verbose && !opts.ShowProgress {
			fmt.Printf("    Fetching page %d...\n", pageCount)
		}

		// Fetch registry data
		resp, err := c.HTTPClient.Get(fetchURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch page %d: %w", pageCount, err)
		}

		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return nil, fmt.Errorf("unexpected status code on page %d: %d", pageCount, resp.StatusCode)
		}

		// Read response body
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response on page %d: %w", pageCount, err)
		}

		// Parse JSON
		var registryResp types.RegistryResponse
		if err := json.Unmarshal(body, &registryResp); err != nil {
			return nil, fmt.Errorf("failed to parse JSON on page %d: %w", pageCount, err)
		}

		// Filter servers by status (only keep "active" servers)
		activeServers := make([]types.ServerEntry, 0, len(registryResp.Servers))
		for _, server := range registryResp.Servers {
			if server.Server.Status == "" || server.Server.Status == "active" {
				activeServers = append(activeServers, server)
			}
		}

		allServers = append(allServers, activeServers...)

		if opts.ShowProgress && bar != nil {
			_ = bar.Add(len(activeServers))
		}

		if opts.Verbose && !opts.ShowProgress {
			fmt.Printf("    Found %d active servers on this page\n", len(activeServers))
		}

		// Check if there are more pages
		if registryResp.Metadata.NextCursor == "" {
			break
		}

		cursor = registryResp.Metadata.NextCursor
	}

	if opts.ShowProgress && bar != nil {
		_ = bar.Finish()
		fmt.Println() // Add newline after progress bar
	}

	return allServers, nil
}

// FetchServer fetches a server by name and (optionally) version
// If version is empty, it will fetch the latest version
func (c *Client) FetchServer(baseURL string, name string, version string) (*types.ServerEntry, error) {
	// Construct the endpoint: /v0/servers/{serverName}/versions/{version}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v0/servers") {
		baseURL = baseURL + "/v0/servers"
	}

	if version == "" {
		version = "latest"
	}

	encodedName := url.PathEscape(name)
	fetchURL := fmt.Sprintf("%s/%s/versions/%s", baseURL, encodedName, version)

	resp, err := c.HTTPClient.Get(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server by name: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check HTTP status code before attempting to decode
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var registryResp types.RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode server list response: %w", err)
	}

	if len(registryResp.Servers) == 0 {
		return nil, fmt.Errorf("server not found: %s with version %s", name, version)
	}

	// based on name + version, there should only be one server
	return &registryResp.Servers[0], nil
}

// FetchServerVersions fetches all versions for a specific server
func (c *Client) FetchServerVersions(baseURL string, serverName string) ([]types.ServerEntry, error) {
	// Construct the endpoint: /v0/servers/{serverName}/versions
	baseURL = strings.TrimSuffix(baseURL, "/")
	if !strings.HasSuffix(baseURL, "/v0/servers") {
		baseURL = baseURL + "/v0/servers"
	}

	encodedName := url.PathEscape(serverName)
	fetchURL := fmt.Sprintf("%s/%s/versions", baseURL, encodedName)

	resp, err := c.HTTPClient.Get(fetchURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch server versions: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// Check HTTP status code before attempting to decode
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var registryResp types.RegistryResponse
	if err := json.NewDecoder(resp.Body).Decode(&registryResp); err != nil {
		return nil, fmt.Errorf("failed to decode server versions response: %w", err)
	}

	return registryResp.Servers, nil
}

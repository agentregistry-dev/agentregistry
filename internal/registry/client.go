package registry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/schollz/progressbar/v3"
)

// RegistryResponse represents the response from the MCP registry
type RegistryResponse struct {
	Servers  []ServerEntry    `json:"servers"`
	Metadata RegistryMetadata `json:"metadata"`
}

// RegistryMetadata contains pagination information
type RegistryMetadata struct {
	Count      int    `json:"count"`
	NextCursor string `json:"nextCursor"`
}

// ServerEntry represents a server entry in the registry
type ServerEntry struct {
	Server ServerSpec      `json:"server"`
	Meta   json.RawMessage `json:"_meta"`
}

// ServerSpec represents the server specification
type ServerSpec struct {
	Name        string              `json:"name"`
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Version     string              `json:"version"`
	Status      string              `json:"status"`
	WebsiteURL  string              `json:"websiteUrl"`
	Repository  Repository          `json:"repository"`
	Packages    []ServerPackageInfo `json:"packages"`
}

// ServerPackageInfo represents package information from the server spec
type ServerPackageInfo struct {
	RegistryType string `json:"registryType"`
	Identifier   string `json:"identifier"`
	Version      string `json:"version"`
	Transport    struct {
		Type string `json:"type"`
	} `json:"transport"`
}

// Repository represents the repository information
type Repository struct {
	URL    string `json:"url"`
	Source string `json:"source"`
}

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

	var registryResp RegistryResponse
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
func (c *Client) FetchAllServers(baseURL string, opts FetchOptions) ([]ServerEntry, error) {
	var allServers []ServerEntry
	cursor := ""
	pageCount := 0
	const pageLimit = 100

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
		var registryResp RegistryResponse
		if err := json.Unmarshal(body, &registryResp); err != nil {
			return nil, fmt.Errorf("failed to parse JSON on page %d: %w", pageCount, err)
		}

		// Filter servers by status (only keep "active" servers)
		activeServers := make([]ServerEntry, 0, len(registryResp.Servers))
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


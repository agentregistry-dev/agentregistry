package client

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Client is a lightweight API client replacing the previous SQLite backend
type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string

	// In-memory placeholder registries state (non-persistent)
	regMu      sync.Mutex
	registries []models.Registry
	nextRegID  int
}

const (
	defaultRegistryName = "local"
	defaultBaseURL      = "http://localhost:12121/v0"
)

// NewClientFromEnv constructs a client using environment variables
func NewClientFromEnv() (*Client, error) {
	base := os.Getenv("ARCTL_API_BASE_URL")
	if strings.TrimSpace(base) == "" {
		base = defaultBaseURL
	}
	token := os.Getenv("ARCTL_API_TOKEN")
	c := &Client{
		baseURL: base,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		nextRegID: 1,
	}
	// Verify connectivity
	if err := c.Ping(); err != nil {
		return nil, fmt.Errorf("failed to reach API at %s: %w", base, err)
	}
	// Seed placeholder registry entry
	c.regMu.Lock()
	now := time.Now()
	if len(c.registries) == 0 {
		c.registries = []models.Registry{
			{
				ID:        c.nextRegID,
				Name:      defaultRegistryName,
				URL:       base,
				Type:      "api",
				CreatedAt: now,
				UpdatedAt: now,
			},
		}
		c.nextRegID++
	}
	c.regMu.Unlock()
	return c, nil
}

// NewClient constructs a client with explicit baseURL and token
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		baseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		nextRegID: 1,
	}
}

// Close is a no-op in API mode
func (c *Client) Close() error { return nil }

func (c *Client) newRequest(method, pathWithQuery string) (*http.Request, error) {
	fullURL := strings.TrimRight(c.baseURL, "/") + pathWithQuery
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	return req, nil
}

func (c *Client) doJSON(req *http.Request, out interface{}) error {
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("unexpected status: %s", resp.Status)
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

// Ping checks connectivity to the API
func (c *Client) Ping() error {
	req, err := c.newRequest(http.MethodGet, "/ping")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// GetRegistries returns all connected registries
func (c *Client) GetRegistries() ([]models.Registry, error) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	out := make([]models.Registry, len(c.registries))
	copy(out, c.registries)
	return out, nil
}

// GetServers returns all MCP servers from connected registries
func (c *Client) GetServers() ([]models.ServerDetail, error) {
	// Cursor-based pagination to fetch all servers
	limit := 100
	cursor := ""
	var all []models.ServerDetail

	for {
		q := fmt.Sprintf("/servers?limit=%d", limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(http.MethodGet, q)
		if err != nil {
			return nil, err
		}

		var resp apiv0.ServerListResponse
		if err := c.doJSON(req, &resp); err != nil {
			return nil, err
		}

		for _, sr := range resp.Servers {
			all = append(all, mapServerResponse(sr))
		}

		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

// GetServerByName returns a server by name
func (c *Client) GetServerByName(name string) (*models.ServerDetail, error) {
	// Use the special "latest" version endpoint
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/servers/"+encName+"/versions/latest")
	if err != nil {
		return nil, err
	}
	var resp apiv0.ServerResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get server by name: %w", err)
	}
	s := mapServerResponse(resp)
	return &s, nil
}

// GetSkills returns all skills from connected registries
func (c *Client) GetSkills() ([]models.Skill, error) {
	limit := 100
	cursor := ""
	var all []models.Skill

	for {
		q := fmt.Sprintf("/skills?limit=%d", limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(http.MethodGet, q)
		if err != nil {
			return nil, err
		}

		var resp models.SkillListResponse
		if err := c.doJSON(req, &resp); err != nil {
			return nil, err
		}
		for _, sk := range resp.Skills {
			all = append(all, mapSkillResponse(sk))
		}
		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

// GetSkillByName returns a skill by name
func (c *Client) GetSkillByName(name string) (*models.Skill, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/skills/"+encName+"/versions/latest")
	if err != nil {
		return nil, err
	}
	var resp models.SkillResponse
	if err := c.doJSON(req, &resp); err != nil {
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill by name: %w", err)
	}
	s := mapSkillResponse(resp)
	return &s, nil
}

// GetAgents returns all agents from connected registries
func (c *Client) GetAgents() ([]models.Agent, error) {
	limit := 100
	cursor := ""
	var all []models.Agent

	for {
		q := fmt.Sprintf("/agents?limit=%d", limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(http.MethodGet, q)
		if err != nil {
			return nil, err
		}

		var resp models.AgentListResponse
		if err := c.doJSON(req, &resp); err != nil {
			return nil, err
		}
		for _, ag := range resp.Agents {
			all = append(all, mapAgentResponse(ag))
		}
		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

func (c *Client) GetAgentByName(name string) (*models.Agent, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/agents/"+encName+"/versions/latest")
	if err != nil {
		return nil, err
	}
	var resp models.AgentResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get agent by name: %w", err)
	}
	s := mapAgentResponse(resp)
	return &s, nil
}

func mapAgentResponse(ag models.AgentResponse) models.Agent {
	// Store the raw agent response as JSON for potential future use
	dataBytes, _ := json.Marshal(ag)
	// Derive category from packages if desired (placeholder empty)
	return models.Agent{
		ID:           0,
		RegistryID:   0,
		RegistryName: defaultRegistryName,
		Name:         ag.Agent.Name,
		Title:        ag.Agent.Title,
		Description:  ag.Agent.Description,
		Version:      ag.Agent.Version,
		Installed:    false,
		Data:         string(dataBytes),
		CreatedAt:    time.Time{},
		UpdatedAt:    time.Time{},
	}
}

// GetInstallations returns all installed resources
func (c *Client) GetInstallations() ([]models.Installation, error) {
	return []models.Installation{}, nil
}

// AddRegistry adds a new registry
func (c *Client) AddRegistry(name, urlStr, registryType string) error {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	// Placeholder: add to in-memory list
	for _, r := range c.registries {
		if r.Name == name {
			return fmt.Errorf("registry already exists: %s", name)
		}
	}
	now := time.Now()
	c.registries = append(c.registries, models.Registry{
		ID:        c.nextRegID,
		Name:      name,
		URL:       urlStr,
		Type:      registryType,
		CreatedAt: now,
		UpdatedAt: now,
	})
	c.nextRegID++
	return nil
}

// GetRegistryByName returns a registry by name
func (c *Client) GetRegistryByName(name string) (*models.Registry, error) {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	for i := range c.registries {
		if c.registries[i].Name == name {
			r := c.registries[i]
			return &r, nil
		}
	}
	return nil, nil
}

// RemoveRegistry removes a registry
func (c *Client) RemoveRegistry(name string) error {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	for i := range c.registries {
		if c.registries[i].Name == name {
			c.registries = append(c.registries[:i], c.registries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("registry not found: %s", name)
}

// AddOrUpdateServer adds or updates a server in the database
func (c *Client) AddOrUpdateServer(registryID int, name, title, description, version, websiteURL, data string) error {
	// No-op in API mode
	return nil
}

// ClearRegistryServers removes all servers for a specific registry
func (c *Client) ClearRegistryServers(registryID int) error { return nil }

// RemoveRegistryByID removes a registry by ID
func (c *Client) RemoveRegistryByID(id string) error {
	c.regMu.Lock()
	defer c.regMu.Unlock()
	rid, err := strconv.Atoi(id)
	if err != nil {
		return fmt.Errorf("invalid registry id: %s", id)
	}
	for i := range c.registries {
		if c.registries[i].ID == rid {
			c.registries = append(c.registries[:i], c.registries[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("registry not found with id: %s", id)
}

// InstallServer marks a server as installed and stores its configuration
func (c *Client) InstallServer(serverID string, config map[string]string) error { return nil }

// UninstallServer marks a server as uninstalled and removes its installation record
func (c *Client) UninstallServer(serverID string) error { return nil }

// (no-op) previously used for local install config marshaling

// MarkServerInstalled marks a server as installed or uninstalled
func (c *Client) MarkServerInstalled(serverID int, installed bool) error { return nil }

// MarkSkillInstalled marks a skill as installed or uninstalled
func (c *Client) MarkSkillInstalled(skillID int, installed bool) error { return nil }

// GetInstalledServers returns all installed MCP servers
func (c *Client) GetInstalledServers() ([]models.ServerDetail, error) {
	return []models.ServerDetail{}, nil
}

// GetInstallationByName returns an installation record by resource name
func (c *Client) GetInstallationByName(resourceType, resourceName string) (*models.Installation, error) {
	return nil, nil
}

// Helpers to convert API errors
func asHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		// No direct status available when using http.Client unless we read resp
		// This helper is best-effort; return 0 (unknown)
		return 0
	}
	return 0
}

// Mapping helpers
func mapServerResponse(sr apiv0.ServerResponse) models.ServerDetail {
	// Build CombinedServerData for downstream consumers (tables, sorting)
	combined := models.CombinedServerData{
		Server: models.ServerFullData{
			Name:        sr.Server.Name,
			Version:     sr.Server.Version,
			Description: sr.Server.Description,
			// Map packages
			Packages: func() []models.ServerPackage {
				out := make([]models.ServerPackage, 0, len(sr.Server.Packages))
				for _, p := range sr.Server.Packages {
					sp := models.ServerPackage{RegistryType: p.RegistryType, Identifier: p.Identifier}
					sp.Transport.Type = p.Transport.Type
					out = append(out, sp)
				}
				return out
			}(),
			// Remotes are optional and may be omitted in responses
			Remotes: nil,
		},
	}
	// Official meta if available
	if sr.Meta.Official != nil {
		combined.Meta.Official.Status = string(sr.Meta.Official.Status)
		combined.Meta.Official.PublishedAt = sr.Meta.Official.PublishedAt
		combined.Meta.Official.UpdatedAt = sr.Meta.Official.UpdatedAt
		combined.Meta.Official.IsLatest = sr.Meta.Official.IsLatest
	}

	dataBytes, _ := json.Marshal(combined)
	return models.ServerDetail{
		// Local ID/Registry fields are not meaningful in API mode
		ID:           0,
		RegistryID:   0,
		RegistryName: defaultRegistryName,
		Name:         sr.Server.Name,
		Title:        sr.Server.Title,
		Description:  sr.Server.Description,
		Version:      sr.Server.Version,
		WebsiteURL:   sr.Server.WebsiteURL,
		Installed:    false,
		Data:         string(dataBytes),
		// Timestamps not provided by API at this level
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
}

func mapSkillResponse(sk models.SkillResponse) models.Skill {
	// Store the raw skill response as JSON for potential future use
	dataBytes, _ := json.Marshal(sk)
	// Derive category from packages if desired (placeholder empty)
	return models.Skill{
		ID:           0,
		RegistryID:   0,
		RegistryName: defaultRegistryName,
		Name:         sk.Skill.Name,
		Title:        sk.Skill.Title,
		Description:  sk.Skill.Description,
		Version:      sk.Skill.Version,
		Category:     "",
		Installed:    false,
		Data:         string(dataBytes),
		CreatedAt:    time.Time{},
		UpdatedAt:    time.Time{},
	}
}

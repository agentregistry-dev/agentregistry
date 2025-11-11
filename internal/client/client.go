package client

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/models"
	internalv0 "github.com/agentregistry-dev/agentregistry/internal/registry/api/handlers/v0"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Client is a lightweight API client replacing the previous SQLite backend
type Client struct {
	BaseURL    string
	httpClient *http.Client
	token      string
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
		BaseURL: base,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
	// Verify connectivity
	// retry backoff with exponential backoff
	for i := 0; i < 5; i++ {
		if err := c.Ping(); err != nil {
			if i == 2 {
				return nil, fmt.Errorf("failed to reach API after 3 attempts: %w", err)
			}
			time.Sleep(time.Duration(i+1) * time.Second)
		}
	}
	// Seed placeholder registry entry
	return c, nil
}

// NewClient constructs a client with explicit baseURL and token
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		BaseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Close is a no-op in API mode
func (c *Client) Close() error { return nil }

func (c *Client) newRequest(method, pathWithQuery string) (*http.Request, error) {
	fullURL := strings.TrimRight(c.BaseURL, "/") + pathWithQuery
	req, err := http.NewRequest(method, fullURL, nil)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	return req, nil
}

func (c *Client) doJSON(req *http.Request, out any) error {
	if out != nil {
		req.Header.Set("Accept", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// read up to 1KB of body for error message
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("unexpected status: %s, %s", resp.Status, string(errBody))
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

func (c *Client) doJsonRequest(method, pathWithQuery string, in, out any) error {
	req, err := c.newRequest(method, pathWithQuery)
	if err != nil {
		return err
	}
	if in != nil {
		inBytes, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("failed to marshal %T: %w", in, err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Body = io.NopCloser(bytes.NewReader(inBytes))
	}
	return c.doJSON(req, out)
}

// Ping checks connectivity to the API
func (c *Client) Ping() error {
	req, err := c.newRequest(http.MethodGet, "/ping")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

func (c *Client) GetVersion() (*internalv0.VersionBody, error) {
	req, err := c.newRequest(http.MethodGet, "/version")
	if err != nil {
		return nil, err
	}
	var resp internalv0.VersionBody
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetServers returns all MCP servers from connected registries
func (c *Client) GetServers() ([]*v0.ServerResponse, error) {
	// Cursor-based pagination to fetch all servers
	limit := 100
	cursor := ""
	var all []*v0.ServerResponse

	for {
		q := fmt.Sprintf("/servers?limit=%d", limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(http.MethodGet, q)
		if err != nil {
			return nil, err
		}

		var resp v0.ServerListResponse
		if err := c.doJSON(req, &resp); err != nil {
			return nil, err
		}

		for _, s := range resp.Servers {
			all = append(all, &s)
		}

		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

// GetServerByName returns a server by name (latest version)
func (c *Client) GetServerByName(name string) (*v0.ServerResponse, error) {
	return c.GetServerByNameAndVersion(name, "latest")
}

// GetServerByNameAndVersion returns a specific version of a server
func (c *Client) GetServerByNameAndVersion(name, version string) (*v0.ServerResponse, error) {
	// Use the version endpoint
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	req, err := c.newRequest(http.MethodGet, "/servers/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}
	var resp v0.ServerResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}
	return &resp, nil
}

// GetServerVersions returns all versions of a server by name
func (c *Client) GetServerVersions(name string) ([]v0.ServerResponse, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/servers/"+encName+"/versions")
	if err != nil {
		return nil, err
	}

	var resp v0.ServerListResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns empty list
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get server versions: %w", err)
	}

	return resp.Servers, nil
}

// GetSkills returns all skills from connected registries
func (c *Client) GetSkills() ([]*models.SkillResponse, error) {
	limit := 100
	cursor := ""
	var all []*models.SkillResponse

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
			all = append(all, &sk)
		}
		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

// GetSkillByName returns a skill by name
func (c *Client) GetSkillByName(name string) (*models.SkillResponse, error) {
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
	return &resp, nil
}

// GetAgents returns all agents from connected registries
func (c *Client) GetAgents() ([]*models.AgentResponse, error) {
	limit := 100
	cursor := ""
	var all []*models.AgentResponse

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
			all = append(all, &ag)
		}
		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

func (c *Client) GetAgentByName(name string) (*models.AgentResponse, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/agents/"+encName+"/versions/latest")
	if err != nil {
		return nil, err
	}
	var resp models.AgentResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, fmt.Errorf("failed to get agent by name: %w", err)
	}
	return &resp, nil
}

// PublishSkill publishes a skill to the registry
func (c *Client) PublishSkill(skill *models.SkillJSON) (*models.SkillResponse, error) {
	var resp models.SkillResponse
	err := c.doJsonRequest(http.MethodPost, "/skills/publish", skill, &resp)
	return &resp, err
}

// PublishAgent publishes an agent to the registry
func (c *Client) PublishAgent(agent *models.AgentJSON) (*models.AgentResponse, error) {
	var resp models.AgentResponse
	err := c.doJsonRequest(http.MethodPost, "/agents/publish", agent, &resp)
	return &resp, err
}

// PublishServer publishes a server to the registry
func (c *Client) PublishServer(server *v0.ServerJSON) (*v0.ServerResponse, error) {
	var resp v0.ServerResponse
	err := c.doJsonRequest(http.MethodPost, "/publish", server, &resp)
	return &resp, err
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

// DeploymentResponse represents a deployment returned by the API
type DeploymentResponse struct {
	ServerName   string            `json:"serverName"`
	Version      string            `json:"version"`
	DeployedAt   string            `json:"deployedAt"`
	UpdatedAt    string            `json:"updatedAt"`
	Status       string            `json:"status"`
	Config       map[string]string `json:"config"`
	PreferRemote bool              `json:"preferRemote"`
	ResourceType string            `json:"resourceType"`
}

// DeploymentsListResponse represents the list of deployments
type DeploymentsListResponse struct {
	Deployments []DeploymentResponse `json:"deployments"`
}

// GetDeployedServers retrieves all deployed servers
func (c *Client) GetDeployedServers() ([]*DeploymentResponse, error) {
	req, err := c.newRequest(http.MethodGet, "/deployments")
	if err != nil {
		return nil, err
	}

	var resp DeploymentsListResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}

	// Convert to pointer slice
	result := make([]*DeploymentResponse, len(resp.Deployments))
	for i := range resp.Deployments {
		result[i] = &resp.Deployments[i]
	}

	return result, nil
}

// GetDeployedServerByName retrieves a specific deployment
func (c *Client) GetDeployedServerByName(name string) (*DeploymentResponse, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/deployments/"+encName)
	if err != nil {
		return nil, err
	}

	var deployment DeploymentResponse
	if err := c.doJSON(req, &deployment); err != nil {
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	return &deployment, nil
}

// DeployServer deploys a server with configuration
func (c *Client) DeployServer(name, version string, config map[string]string, preferRemote bool) (*DeploymentResponse, error) {
	payload := map[string]interface{}{
		"serverName":   name,
		"version":      version,
		"config":       config,
		"preferRemote": preferRemote,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPost, "/deployments", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// UpdateDeploymentConfig updates deployment configuration
func (c *Client) UpdateDeploymentConfig(name string, config map[string]string) (*DeploymentResponse, error) {
	encName := url.PathEscape(name)
	payload := map[string]interface{}{
		"config": config,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPut, "/deployments/"+encName+"/config", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// RemoveServer removes a deployment
func (c *Client) RemoveServer(name string) error {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodDelete, "/deployments/"+encName)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

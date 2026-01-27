package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
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
	DefaultBaseURL      = defaultBaseURL
)

// NewClientFromEnv constructs a client using environment variables
func NewClientFromEnv() (*Client, error) {
	return NewClientWithConfig(os.Getenv("ARCTL_API_BASE_URL"), os.Getenv("ARCTL_API_TOKEN"))
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

// NewClientWithConfig constructs a client from explicit inputs (flag/env), applies defaults, and verifies connectivity.
func NewClientWithConfig(baseURL, token string) (*Client, error) {
	base := strings.TrimSpace(baseURL)
	if base == "" {
		base = defaultBaseURL
	}

	c := NewClient(base, token)
	if err := pingWithRetry(c); err != nil {
		return nil, fmt.Errorf("failed to reach API at %s: %w", c.BaseURL, err)
	}

	return c, nil
}

func pingWithRetry(c *Client) error {
	var lastErr error
	const attempts = 3
	for i := range attempts {
		if err := c.Ping(); err != nil {
			lastErr = err
			time.Sleep(time.Duration(i+1) * time.Second)
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to reach API after %d attempts: %w", attempts, lastErr)
}

// Close is a no-op in API mode
func (c *Client) Close() error { return nil }

func (c *Client) baseURLWithoutVersion() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if strings.HasSuffix(base, "/v0") {
		return base[:len(base)-3]
	}
	if strings.HasSuffix(base, "/v0.1") {
		return base[:len(base)-5]
	}
	return base
}

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

func (c *Client) newAdminRequest(method, pathWithQuery string) (*http.Request, error) {
	base := c.baseURLWithoutVersion()
	fullURL := strings.TrimRight(base, "/") + pathWithQuery
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

func (c *Client) GetAllServers() ([]*v0.ServerResponse, error) {
	limit := 100
	cursor := ""
	var all []*v0.ServerResponse

	for {
		req, err := c.newAdminRequest(http.MethodGet, "/admin/v0/servers?limit="+strconv.Itoa(limit)+"&cursor="+url.QueryEscape(cursor))
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

// GetPublishedServers returns all published MCP servers
func (c *Client) GetPublishedServers() ([]*v0.ServerResponse, error) {
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
func (c *Client) GetServerByName(name string, publishedOnly bool) (*v0.ServerResponse, error) {
	return c.GetServerByNameAndVersion(name, "latest", publishedOnly)
}

// GetServerByNameAndVersion returns a specific version of a server
func (c *Client) GetServerByNameAndVersion(name, version string, publishedOnly bool) (*v0.ServerResponse, error) {
	// Use the version endpoint
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	q := "/servers/" + encName + "/versions/" + encVersion
	if publishedOnly {
		q += "?published_only=true"
	}
	req, err := c.newRequest(http.MethodGet, q)
	if err != nil {
		return nil, err
	}
	// The endpoint now returns ServerListResponse (even for a single version)
	var resp v0.ServerListResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}

	if len(resp.Servers) == 0 {
		return nil, nil
	}

	return &resp.Servers[0], nil
}

// GetServerVersions returns all versions of a server by name (public endpoint - only published)
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

// GetAllServerVersionsAdmin returns all versions of a server by name (admin endpoint - includes unpublished)
func (c *Client) GetAllServerVersionsAdmin(name string) ([]v0.ServerResponse, error) {
	encName := url.PathEscape(name)

	req, err := c.newAdminRequest(http.MethodGet, "/admin/v0/servers/"+encName+"/versions")
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

// GetAgentByNameAndVersion returns a specific version of an agent
func (c *Client) GetAgentByNameAndVersion(name, version string) (*models.AgentResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	req, err := c.newRequest(http.MethodGet, "/agents/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}
	var resp models.AgentResponse
	if err := c.doJSON(req, &resp); err != nil {
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get agent by name and version: %w", err)
	}
	return &resp, nil
}

// PushSkill creates a skill entry in the registry without publishing (published=false)
func (c *Client) PushSkill(skill *models.SkillJSON) (*models.SkillResponse, error) {
	var resp models.SkillResponse
	err := c.doJsonRequest(http.MethodPost, "/skills/publish", skill, &resp)
	return &resp, err
}

// PublishSkillStatus marks an existing skill as published (sets published=true)
func (c *Client) PublishSkillStatus(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/skills/"+encName+"/versions/"+encVersion+"/publish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// PublishSkill creates a skill entry and marks it as published (published=true)
func (c *Client) PublishSkill(skill *models.SkillJSON) (*models.SkillResponse, error) {
	if _, err := c.PushSkill(skill); err != nil {
		return nil, err
	}

	// Then mark it as published
	if err := c.PublishSkillStatus(skill.Name, skill.Version); err != nil {
		return nil, fmt.Errorf("failed to publish skill: %w", err)
	}

	// Fetch the updated skill to return it
	return c.GetSkillByNameAndVersion(skill.Name, skill.Version)
}

// PushAgent creates an agent entry in the registry without publishing (published=false)
func (c *Client) PushAgent(agent *models.AgentJSON) (*models.AgentResponse, error) {
	var resp models.AgentResponse
	// Use a dedicated /agents/push public endpoint for push (creates unpublished entry)
	err := c.doJsonRequest(http.MethodPost, "/agents/push", agent, &resp)
	return &resp, err
}

// PublishAgentStatus marks an existing agent as published (sets published=true)
func (c *Client) PublishAgentStatus(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/agents/"+encName+"/versions/"+encVersion+"/publish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// UnpublishAgentStatus marks an existing agent as unpublished (sets published=false)
func (c *Client) UnpublishAgentStatus(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/agents/"+encName+"/versions/"+encVersion+"/unpublish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// PublishAgent creates an agent entry and marks it as published (published=true)
func (c *Client) PublishAgent(agent *models.AgentJSON) (*models.AgentResponse, error) {
	// First create the agent (published=false)
	if _, err := c.PushAgent(agent); err != nil {
		return nil, err
	}

	// Then mark it as published
	if err := c.PublishAgentStatus(agent.Name, agent.Version); err != nil {
		return nil, fmt.Errorf("failed to publish agent: %w", err)
	}

	// Fetch the updated agent to return it
	return c.GetAgentByNameAndVersion(agent.Name, agent.Version)
}

// PushMCPServer creates an MCP server entry in the registry without publishing (published=false)
func (c *Client) PushMCPServer(server *v0.ServerJSON) (*v0.ServerResponse, error) {
	var resp v0.ServerResponse
	err := c.doJsonRequest(http.MethodPost, "/servers/push", server, &resp)
	return &resp, err
}

// PublishMCPServerStatus marks an existing MCP server as published (sets published=true)
func (c *Client) PublishMCPServerStatus(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/servers/"+encName+"/versions/"+encVersion+"/publish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// PublishMCPServer creates an MCP server entry and marks it as published (published=true)
func (c *Client) PublishMCPServer(server *v0.ServerJSON) (*v0.ServerResponse, error) {
	// First create the server (published=false)
	if _, err := c.PushMCPServer(server); err != nil {
		return nil, err
	}

	// Then mark it as published
	if err := c.PublishMCPServerStatus(server.Name, server.Version); err != nil {
		return nil, fmt.Errorf("failed to publish mcp server: %w", err)
	}

	// Fetch the published server to return it
	return c.GetServerByNameAndVersion(server.Name, server.Version, true)
}

// UnpublishMCPServer unpublishes an MCP server from the registry
func (c *Client) UnpublishMCPServer(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/servers/"+encName+"/versions/"+encVersion+"/unpublish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// UnpublishSkill unpublishes a skill from the registry
func (c *Client) UnpublishSkill(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodPost, "/admin/v0/skills/"+encName+"/versions/"+encVersion+"/unpublish")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// GetSkillVersions returns all versions of a skill by name (admin endpoint - includes unpublished)
func (c *Client) GetSkillVersions(name string) ([]*models.SkillResponse, error) {
	encName := url.PathEscape(name)

	req, err := c.newAdminRequest(http.MethodGet, "/admin/v0/skills/"+encName+"/versions")
	if err != nil {
		return nil, err
	}

	var resp models.SkillListResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns empty list
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill versions: %w", err)
	}

	// Convert []SkillResponse to []*SkillResponse
	result := make([]*models.SkillResponse, len(resp.Skills))
	for i := range resp.Skills {
		result[i] = &resp.Skills[i]
	}

	return result, nil
}

// GetSkillByNameAndVersion returns a specific version of a skill
func (c *Client) GetSkillByNameAndVersion(name, version string) (*models.SkillResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newRequest(http.MethodGet, "/skills/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}

	var resp models.SkillResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill by name and version: %w", err)
	}

	return &resp, nil
}

// GetSkillByNameAndVersionAdmin returns a specific version of a skill (admin endpoint - includes unpublished)
func (c *Client) GetSkillByNameAndVersionAdmin(name, version string) (*models.SkillResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodGet, "/admin/v0/skills/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}

	var resp models.SkillResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get skill by name and version: %w", err)
	}

	return &resp, nil
}

// GetAgentByNameAndVersionAdmin returns a specific version of an agent (admin endpoint - includes unpublished)
func (c *Client) GetAgentByNameAndVersionAdmin(name, version string) (*models.AgentResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodGet, "/admin/v0/agents/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}

	var resp models.AgentResponse
	if err := c.doJSON(req, &resp); err != nil {
		// 404 -> not found returns nil
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get agent by name and version: %w", err)
	}

	return &resp, nil
}

// DeleteAgent deletes an agent from the registry
func (c *Client) DeleteAgent(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodDelete, "/admin/v0/agents/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

// DeleteSkill deletes a skill from the registry
// Note: This uses DELETE HTTP method. If the endpoint doesn't exist, it will return an error.
func (c *Client) DeleteSkill(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodDelete, "/admin/v0/skills/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

// DeleteMCPServer deletes an MCP server from the registry by setting its status to deleted
func (c *Client) DeleteMCPServer(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newAdminRequest(http.MethodDelete, "/admin/v0/servers/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// Helpers to convert API errors
func asHTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	errStr := err.Error()
	// Parse error format: "unexpected status: 404 Not Found, ..."
	// Extract status code from the error message
	if strings.Contains(errStr, "unexpected status:") {
		parts := strings.Split(errStr, "unexpected status: ")
		if len(parts) > 1 {
			statusPart := strings.Split(parts[1], " ")[0]
			if code, parseErr := strconv.Atoi(statusPart); parseErr == nil {
				return code
			}
		}
	}
	// Also check for "404" or "Not Found" in the error message
	if strings.Contains(errStr, "404") || strings.Contains(errStr, "Not Found") {
		return http.StatusNotFound
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
	Runtime      string            `json:"runtime"`
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

// GetDeployedServerByNameAndVersion retrieves a specific deployment by name and version
func (c *Client) GetDeployedServerByNameAndVersion(name string, version string) (*DeploymentResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	url := fmt.Sprintf("/deployments/%s/versions/%s", encName, encVersion)
	req, err := c.newRequest(http.MethodGet, url)
	if err != nil {
		return nil, err
	}

	var deployment DeploymentResponse
	if err := c.doJSON(req, &deployment); err != nil {
		if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "Not Found") {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	return &deployment, nil
}

// DeployServer deploys a server with configuration
func (c *Client) DeployServer(name, version string, config map[string]string, preferRemote bool, runtimeTarget string) (*DeploymentResponse, error) {
	payload := internalv0.DeploymentRequest{
		ServerName:   name,
		Version:      version,
		Config:       config,
		PreferRemote: preferRemote,
		ResourceType: "mcp",
		Runtime:      runtimeTarget,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPost, "/deployments", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// DeployAgent deploys an agent with configuration
func (c *Client) DeployAgent(name, version string, config map[string]string, runtimeTarget string) (*DeploymentResponse, error) {
	payload := internalv0.DeploymentRequest{
		ServerName:   name,
		Version:      version,
		Config:       config,
		ResourceType: "agent",
		Runtime:      runtimeTarget,
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
	payload := map[string]any{
		"config": config,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPut, "/deployments/"+encName+"/config", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// RemoveServer removes a deployment
func (c *Client) RemoveServer(name string, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	req, err := c.newRequest(http.MethodDelete, "/deployments/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

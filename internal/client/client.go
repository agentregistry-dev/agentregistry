package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

// Client is a lightweight API client replacing the previous SQLite backend
type Client struct {
	BaseURL    string
	httpClient *http.Client
	token      string
}

const (
	defaultBaseURL          = "http://localhost:12121/v0"
	DefaultBaseURL          = defaultBaseURL
	defaultDeployProviderID = "local"
)

type VersionBody = apitypes.VersionBody

type deploymentRequest = apitypes.DeploymentRequest

type IndexRequest = apitypes.IndexRequest

type IndexJobResponse = apitypes.IndexJobResponse

type JobProgress = apitypes.JobProgress

type JobResult = apitypes.JobResult

type JobStatusResponse = apitypes.JobStatusResponse

type DeploymentResponse = models.Deployment

type DeploymentsListResponse = apitypes.DeploymentsListResponse

// NewClientFromEnv constructs a client using environment variables
func NewClientFromEnv() (*Client, error) {
	base := os.Getenv("ARCTL_API_BASE_URL")
	token := os.Getenv("ARCTL_API_TOKEN")
	return NewClientWithConfig(base, token)
}

// NewClient constructs a client with explicit baseURL and token.
// The baseURL can be provided with or without the /v0 API prefix;
// if missing, /v0 is appended automatically.
func NewClient(baseURL, token string) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	baseURL = ensureV0Suffix(baseURL)
	return &Client{
		BaseURL: baseURL,
		token:   token,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ensureV0Suffix appends /v0 to the URL if not already present.
func ensureV0Suffix(u string) string {
	u = strings.TrimRight(u, "/")
	if !strings.HasSuffix(u, "/v0") {
		u += "/v0"
	}
	return u
}

// NewClientWithConfig constructs a client from explicit inputs (flag/env), applies defaults, and verifies connectivity.
func NewClientWithConfig(baseURL, token string) (*Client, error) {
	c := NewClient(baseURL, token)
	if err := c.Ping(); err != nil {
		return nil, err
	}
	return c, nil
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
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if msg := extractAPIErrorMessage(errBody); msg != "" {
			return fmt.Errorf("%s: %s", resp.Status, msg)
		}
		return fmt.Errorf("unexpected status: %s, %s", resp.Status, string(errBody))
	}
	if out == nil {
		return nil
	}
	dec := json.NewDecoder(resp.Body)
	return dec.Decode(out)
}

// extractAPIErrorMessage parses a Huma-style JSON error body and returns a
// human-readable string with just the error messages. Returns "" if the body
// cannot be parsed.
func extractAPIErrorMessage(body []byte) string {
	var apiErr struct {
		Detail string `json:"detail"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &apiErr) != nil || (apiErr.Detail == "" && len(apiErr.Errors) == 0) {
		return ""
	}
	var msgs []string
	for _, e := range apiErr.Errors {
		if e.Message != "" {
			msgs = append(msgs, e.Message)
		}
	}
	if len(msgs) > 0 {
		return strings.Join(msgs, "; ")
	}
	return apiErr.Detail
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

func (c *Client) GetVersion() (*VersionBody, error) {
	req, err := c.newRequest(http.MethodGet, "/version")
	if err != nil {
		return nil, err
	}
	var resp VersionBody
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
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

// GetServer returns a server by name (latest version)
func (c *Client) GetServer(name string) (*v0.ServerResponse, error) {
	return c.GetServerVersion(name, "latest")
}

// GetServerVersion returns a specific version of a server
func (c *Client) GetServerVersion(name, version string) (*v0.ServerResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	q := "/servers/" + encName + "/versions/" + encVersion
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

// GetSkill returns a skill by name
func (c *Client) GetSkill(name string) (*models.SkillResponse, error) {
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

// GetSkillVersions returns all versions for a skill by name.
func (c *Client) GetSkillVersions(name string) ([]*models.SkillResponse, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/skills/"+encName+"/versions")
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

	// Convert to pointer slice to match existing client method conventions.
	result := make([]*models.SkillResponse, len(resp.Skills))
	for i := range resp.Skills {
		result[i] = &resp.Skills[i]
	}

	return result, nil
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

func (c *Client) GetAgent(name string) (*models.AgentResponse, error) {
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

// GetAgentVersion returns a specific version of an agent
func (c *Client) GetAgentVersion(name, version string) (*models.AgentResponse, error) {
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

// GetPrompts returns all prompts from the registry
func (c *Client) GetPrompts() ([]*models.PromptResponse, error) {
	limit := 100
	cursor := ""
	var all []*models.PromptResponse

	for {
		q := fmt.Sprintf("/prompts?limit=%d", limit)
		if cursor != "" {
			q += "&cursor=" + url.QueryEscape(cursor)
		}
		req, err := c.newRequest(http.MethodGet, q)
		if err != nil {
			return nil, err
		}

		var resp models.PromptListResponse
		if err := c.doJSON(req, &resp); err != nil {
			return nil, err
		}
		for _, p := range resp.Prompts {
			all = append(all, &p)
		}
		if resp.Metadata.NextCursor == "" {
			break
		}
		cursor = resp.Metadata.NextCursor
	}

	return all, nil
}

// GetPrompt returns a prompt by name (latest version)
func (c *Client) GetPrompt(name string) (*models.PromptResponse, error) {
	encName := url.PathEscape(name)
	req, err := c.newRequest(http.MethodGet, "/prompts/"+encName+"/versions/latest")
	if err != nil {
		return nil, err
	}
	var resp models.PromptResponse
	if err := c.doJSON(req, &resp); err != nil {
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get prompt by name: %w", err)
	}
	return &resp, nil
}

// GetPromptVersion returns a specific version of a prompt
func (c *Client) GetPromptVersion(name, version string) (*models.PromptResponse, error) {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)
	req, err := c.newRequest(http.MethodGet, "/prompts/"+encName+"/versions/"+encVersion)
	if err != nil {
		return nil, err
	}
	var resp models.PromptResponse
	if err := c.doJSON(req, &resp); err != nil {
		if respErr := asHTTPStatus(err); respErr == http.StatusNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get prompt by name and version: %w", err)
	}
	return &resp, nil
}

// CreatePrompt creates a prompt in the registry (immediately visible)
func (c *Client) CreatePrompt(prompt *models.PromptJSON) (*models.PromptResponse, error) {
	var resp models.PromptResponse
	err := c.doJsonRequest(http.MethodPost, "/prompts", prompt, &resp)
	return &resp, err
}

// DeletePrompt deletes a prompt from the registry
func (c *Client) DeletePrompt(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newRequest(http.MethodDelete, "/prompts/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

// CreateSkill creates a skill in the registry (immediately visible)
func (c *Client) CreateSkill(skill *models.SkillJSON) (*models.SkillResponse, error) {
	var resp models.SkillResponse
	err := c.doJsonRequest(http.MethodPost, "/skills", skill, &resp)
	return &resp, err
}

// CreateAgent creates or updates an agent entry.
func (c *Client) CreateAgent(agent *models.AgentJSON) (*models.AgentResponse, error) {
	var resp models.AgentResponse
	err := c.doJsonRequest(http.MethodPost, "/agents", agent, &resp)
	return &resp, err
}

// CreateMCPServer creates or updates an MCP server entry.
func (c *Client) CreateMCPServer(server *v0.ServerJSON) (*v0.ServerResponse, error) {
	var resp v0.ServerResponse
	err := c.doJsonRequest(http.MethodPost, "/servers", server, &resp)
	return &resp, err
}

// GetSkillVersion returns a specific version of a skill
func (c *Client) GetSkillVersion(name, version string) (*models.SkillResponse, error) {
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

// DeleteAgent deletes an agent from the registry
func (c *Client) DeleteAgent(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newRequest(http.MethodDelete, "/agents/"+encName+"/versions/"+encVersion)
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

	req, err := c.newRequest(http.MethodDelete, "/skills/"+encName+"/versions/"+encVersion)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

// DeleteMCPServer deletes an MCP server from the registry by setting its status to deleted
func (c *Client) DeleteMCPServer(name, version string) error {
	encName := url.PathEscape(name)
	encVersion := url.PathEscape(version)

	req, err := c.newRequest(http.MethodDelete, "/servers/"+encName+"/versions/"+encVersion)
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

	// Try "unexpected status: CODE ..." (unparsed JSON fallback)
	if strings.Contains(errStr, "unexpected status:") {
		parts := strings.Split(errStr, "unexpected status: ")
		if len(parts) > 1 {
			statusPart := strings.Split(parts[1], " ")[0]
			if code, parseErr := strconv.Atoi(statusPart); parseErr == nil {
				return code
			}
		}
	}

	// Try "CODE Status Text: message" (parsed API error)
	if code, parseErr := strconv.Atoi(strings.Split(errStr, " ")[0]); parseErr == nil {
		return code
	}

	if strings.Contains(errStr, "404") || strings.Contains(errStr, "Not Found") {
		return http.StatusNotFound
	}
	return 0
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

// GetDeployment retrieves a deployment by ID.
func (c *Client) GetDeployment(id string) (*DeploymentResponse, error) {
	encID := url.PathEscape(id)
	req, err := c.newRequest(http.MethodGet, "/deployments/"+encID)
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

// DeployServer deploys a server with deployment environment variables.
func (c *Client) DeployServer(name, version string, env map[string]string, preferRemote bool, providerID string) (*DeploymentResponse, error) {
	if strings.TrimSpace(providerID) == "" {
		providerID = defaultDeployProviderID
	}
	payload := deploymentRequest{
		ServerName:   name,
		Version:      version,
		Env:          env,
		PreferRemote: preferRemote,
		ResourceType: "mcp",
		ProviderID:   providerID,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPost, "/deployments", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// DeployAgent deploys an agent with deployment environment variables.
func (c *Client) DeployAgent(name, version string, env map[string]string, providerID string) (*DeploymentResponse, error) {
	if strings.TrimSpace(providerID) == "" {
		providerID = defaultDeployProviderID
	}
	payload := deploymentRequest{
		ServerName:   name,
		Version:      version,
		Env:          env,
		ResourceType: "agent",
		ProviderID:   providerID,
	}

	var deployment DeploymentResponse
	if err := c.doJsonRequest(http.MethodPost, "/deployments", payload, &deployment); err != nil {
		return nil, err
	}

	return &deployment, nil
}

// DeleteDeployment removes a deployment by ID.
func (c *Client) DeleteDeployment(id string) error {
	encID := url.PathEscape(id)
	req, err := c.newRequest(http.MethodDelete, "/deployments/"+encID)
	if err != nil {
		return err
	}

	return c.doJSON(req, nil)
}

// SSEClient returns the HTTP client used for SSE requests.
func (c *Client) SSEClient() *http.Client {
	return &http.Client{
		Transport:     c.httpClient.Transport,
		CheckRedirect: c.httpClient.CheckRedirect,
		Jar:           c.httpClient.Jar,
		Timeout:       0,
	}
}

// NewSSERequest creates a request for streaming embedding indexing events.
func (c *Client) NewSSERequest(ctx context.Context, reqBody IndexRequest) (*http.Request, error) {
	req, err := c.newRequest(http.MethodPost, "/embeddings/index/stream")
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal index request: %w", err)
	}
	req = req.WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Body = io.NopCloser(bytes.NewReader(body))
	return req, nil
}

// StartIndex starts a non-streaming indexing job.
func (c *Client) StartIndex(req IndexRequest) (*IndexJobResponse, error) {
	var resp IndexJobResponse
	if err := c.doJsonRequest(http.MethodPost, "/embeddings/index", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetIndexStatus fetches indexing job status by job ID.
func (c *Client) GetIndexStatus(jobID string) (*JobStatusResponse, error) {
	encJobID := url.PathEscape(jobID)
	req, err := c.newRequest(http.MethodGet, "/embeddings/index/"+encJobID)
	if err != nil {
		return nil, err
	}
	var resp JobStatusResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

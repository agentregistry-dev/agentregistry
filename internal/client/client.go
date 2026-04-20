package client

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	apitypes "github.com/agentregistry-dev/agentregistry/internal/registry/api/apitypes"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
)

// Client is a lightweight API client for the agentregistry HTTP surface.
// Every resource method speaks v1alpha1 at /v0/namespaces/{ns}/{plural}/...;
// only deployment + embeddings RPCs continue to use legacy paths until their
// respective Group ports land (Group 4 for deployments, Group 8 for
// embeddings).
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

// ErrNotFound is returned by Get / GetLatest / Delete / PatchStatus when
// the server responds with 404. Callers can errors.Is(err, ErrNotFound)
// to branch cleanly.
var ErrNotFound = errors.New("resource not found")

// NewClientFromEnv constructs a client using environment variables.
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

// NewClientWithConfig constructs a client from explicit inputs (flag/env),
// applies defaults, and verifies connectivity.
func NewClientWithConfig(baseURL, token string) (*Client, error) {
	c := NewClient(baseURL, token)
	if err := c.Ping(); err != nil {
		return nil, err
	}
	return c, nil
}

// Close is a no-op in API mode.
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
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
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
	return json.NewDecoder(resp.Body).Decode(out)
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

// =============================================================================
// Connectivity / version
// =============================================================================

// Ping checks connectivity to the API.
func (c *Client) Ping() error {
	req, err := c.newRequest(http.MethodGet, "/ping")
	if err != nil {
		return err
	}
	return c.doJSON(req, nil)
}

// GetVersion returns the server's version metadata.
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

// =============================================================================
// Generic resource methods — v1alpha1
// =============================================================================

// ListOpts controls the query parameters on List. Namespace "" means
// cross-namespace (GET /v0/{plural}); a non-empty namespace scopes to
// GET /v0/namespaces/{namespace}/{plural}.
type ListOpts struct {
	Namespace          string
	Labels             string
	Limit              int
	Cursor             string
	LatestOnly         bool
	IncludeTerminating bool
}

// listResponse mirrors the resource handler's list envelope shape.
type listResponse struct {
	Items      []v1alpha1.RawObject `json:"items"`
	NextCursor string               `json:"nextCursor,omitempty"`
}

// Get returns the resource at (kind, namespace, name, version). Returns
// ErrNotFound when the row doesn't exist.
func (c *Client) Get(ctx context.Context, kind, namespace, name, version string) (*v1alpha1.RawObject, error) {
	path := fmt.Sprintf("/namespaces/%s/%s/%s/%s",
		url.PathEscape(namespace),
		v1alpha1.PluralFor(kind),
		url.PathEscape(name),
		url.PathEscape(version))
	return c.getRaw(ctx, path)
}

// GetLatest returns the is_latest_version row for (kind, namespace, name).
func (c *Client) GetLatest(ctx context.Context, kind, namespace, name string) (*v1alpha1.RawObject, error) {
	path := fmt.Sprintf("/namespaces/%s/%s/%s",
		url.PathEscape(namespace),
		v1alpha1.PluralFor(kind),
		url.PathEscape(name))
	return c.getRaw(ctx, path)
}

func (c *Client) getRaw(ctx context.Context, path string) (*v1alpha1.RawObject, error) {
	req, err := c.newRequest(http.MethodGet, path)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	var out v1alpha1.RawObject
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns rows of kind, paginated. Empty opts.Namespace lists across
// all namespaces. The returned string is the nextCursor; empty means no
// more pages.
func (c *Client) List(ctx context.Context, kind string, opts ListOpts) ([]v1alpha1.RawObject, string, error) {
	var base string
	if opts.Namespace == "" {
		base = "/" + v1alpha1.PluralFor(kind)
	} else {
		base = fmt.Sprintf("/namespaces/%s/%s", url.PathEscape(opts.Namespace), v1alpha1.PluralFor(kind))
	}
	q := url.Values{}
	if opts.Limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Cursor != "" {
		q.Set("cursor", opts.Cursor)
	}
	if opts.Labels != "" {
		q.Set("labels", opts.Labels)
	}
	if opts.LatestOnly {
		q.Set("latestOnly", "true")
	}
	if opts.IncludeTerminating {
		q.Set("includeTerminating", "true")
	}
	if enc := q.Encode(); enc != "" {
		base += "?" + enc
	}
	req, err := c.newRequest(http.MethodGet, base)
	if err != nil {
		return nil, "", err
	}
	req = req.WithContext(ctx)
	var resp listResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, "", err
	}
	return resp.Items, resp.NextCursor, nil
}

// Delete soft-deletes the (kind, namespace, name, version) row. Returns
// ErrNotFound when the row doesn't exist. See Store.Delete for the
// soft-delete + finalizer semantics.
func (c *Client) Delete(ctx context.Context, kind, namespace, name, version string) error {
	path := fmt.Sprintf("/namespaces/%s/%s/%s/%s",
		url.PathEscape(namespace),
		v1alpha1.PluralFor(kind),
		url.PathEscape(name),
		url.PathEscape(version))
	req, err := c.newRequest(http.MethodDelete, path)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	return c.doJSON(req, nil)
}

// =============================================================================
// Apply batch — multi-doc YAML
// =============================================================================

// ApplyOpts carries cross-cutting batch options for the POST /v0/apply endpoint.
type ApplyOpts struct {
	Force  bool
	DryRun bool
}

// Apply sends a multi-doc YAML body to POST /v0/apply and returns per-resource results.
// Returns an error only on request-level failures (network, 4xx from server).
// Per-resource errors are encoded in the returned results.
func (c *Client) Apply(ctx context.Context, body []byte, opts ApplyOpts) ([]apitypes.ApplyResult, error) {
	return c.applyBatch(ctx, http.MethodPost, body, opts)
}

// DeleteViaApply sends a DELETE /v0/apply with a YAML body and returns per-resource results.
// Mirrors Apply but uses the DELETE HTTP method. DryRun is honored; Force is accepted for
// backwards compatibility but is a no-op under v1alpha1.
func (c *Client) DeleteViaApply(ctx context.Context, body []byte) ([]apitypes.ApplyResult, error) {
	return c.applyBatch(ctx, http.MethodDelete, body, ApplyOpts{})
}

func (c *Client) applyBatch(ctx context.Context, method string, body []byte, opts ApplyOpts) ([]apitypes.ApplyResult, error) {
	u := strings.TrimRight(c.BaseURL, "/") + "/apply"
	q := url.Values{}
	if opts.Force {
		q.Set("force", "true")
	}
	if opts.DryRun {
		q.Set("dryRun", "true")
	}
	if enc := q.Encode(); enc != "" {
		u += "?" + enc
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/yaml")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 400 {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s /v0/apply returned %d: %s", method, resp.StatusCode, string(b))
	}

	var out apitypes.ApplyResultsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding apply response: %w", err)
	}
	return out.Results, nil
}

// =============================================================================
// Deployment RPCs — still served by legacy /v0/deployments/* handlers
// until Group 4 (deployment service v1alpha1 port) lands.
// =============================================================================

// GetDeployedServers retrieves all deployed servers.
func (c *Client) GetDeployedServers() ([]*DeploymentResponse, error) {
	req, err := c.newRequest(http.MethodGet, "/deployments")
	if err != nil {
		return nil, err
	}
	var resp DeploymentsListResponse
	if err := c.doJSON(req, &resp); err != nil {
		return nil, err
	}
	result := make([]*DeploymentResponse, len(resp.Deployments))
	for i := range resp.Deployments {
		result[i] = &resp.Deployments[i]
	}
	return result, nil
}

// GetDeployment retrieves a deployment by ID. Returns (nil, nil) on 404
// to preserve the pre-refactor signature that some CLI callers use as
// an existence check. New callers should prefer the Get path on the
// generic v1alpha1 surface once Group 4 lands.
func (c *Client) GetDeployment(id string) (*DeploymentResponse, error) {
	encID := url.PathEscape(id)
	req, err := c.newRequest(http.MethodGet, "/deployments/"+encID)
	if err != nil {
		return nil, err
	}
	var deployment DeploymentResponse
	if err := c.doJSON(req, &deployment); err != nil {
		if errors.Is(err, ErrNotFound) {
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

// =============================================================================
// Embeddings index — still served by legacy /v0/embeddings handlers until
// Group 8 (indexer port) lands.
// =============================================================================

// SSEClient returns the HTTP client used for SSE requests. Timeout is
// cleared to allow long-lived streams.
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

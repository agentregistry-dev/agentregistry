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
	"strings"
	"time"

	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// Client is a lightweight API client for the agentregistry HTTP surface.
// Every resource method speaks v1alpha1 at /v0/{plural}/{name}/{version}
// with ?namespace=<ns> as an optional query param (namespace is hidden
// from the user-facing API; empty / "default" are elided).
type Client struct {
	BaseURL    string
	httpClient *http.Client
	token      string
}

// DefaultBaseURL is used when NewClient sees an empty base URL. Includes
// the `/v0` API prefix.
const DefaultBaseURL = "http://localhost:12121/v0"

type VersionBody = arv0.VersionBody

// ErrNotFound is returned by Get / GetLatest / Delete / PatchStatus when
// the server responds with 404. Callers can errors.Is(err, ErrNotFound)
// to branch cleanly.
var ErrNotFound = errors.New("resource not found")

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
	return c.newRequestWithBody(method, pathWithQuery, nil, "")
}

// newRequestWithBody is the body-carrying variant used by the apply
// endpoints. contentType is set on the request when non-empty.
func (c *Client) newRequestWithBody(method, pathWithQuery string, body io.Reader, contentType string) (*http.Request, error) {
	fullURL := strings.TrimRight(c.BaseURL, "/") + pathWithQuery
	req, err := http.NewRequest(method, fullURL, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
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

// ListOpts controls the query parameters on List. Namespace "" (empty)
// scopes to the default namespace; "all" widens to every namespace.
// Any other value scopes to that exact namespace.
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

// namespaceQuery appends ?namespace=<ns> to a path when the namespace
// is non-empty and non-default; omitting the query defers to the
// server's default. "all" (the cross-namespace sentinel) only applies
// to list endpoints.
func namespaceQuery(namespace string) string {
	if namespace == "" || namespace == v1alpha1.DefaultNamespace {
		return ""
	}
	return "?namespace=" + url.QueryEscape(namespace)
}

// Get returns the resource at (kind, namespace, name, version). Returns
// ErrNotFound when the row doesn't exist.
func (c *Client) Get(ctx context.Context, kind, namespace, name, version string) (*v1alpha1.RawObject, error) {
	path := fmt.Sprintf("/%s/%s/%s%s",
		v1alpha1.PluralFor(kind),
		url.PathEscape(name),
		url.PathEscape(version),
		namespaceQuery(namespace))
	return c.getRaw(ctx, path)
}

// GetLatest returns the is_latest_version row for (kind, namespace, name).
func (c *Client) GetLatest(ctx context.Context, kind, namespace, name string) (*v1alpha1.RawObject, error) {
	path := fmt.Sprintf("/%s/%s%s",
		v1alpha1.PluralFor(kind),
		url.PathEscape(name),
		namespaceQuery(namespace))
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

// List returns rows of kind, paginated. opts.Namespace="" (empty) lists
// the default namespace; opts.Namespace="all" widens to every
// namespace. The returned string is the nextCursor; empty means no
// more pages.
func (c *Client) List(ctx context.Context, kind string, opts ListOpts) ([]v1alpha1.RawObject, string, error) {
	base := "/" + v1alpha1.PluralFor(kind)
	q := url.Values{}
	if opts.Namespace != "" {
		q.Set("namespace", opts.Namespace)
	}
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

// DeleteOpts carries optional flags for Delete. Zero-value is the
// safe default (provider teardown runs).
type DeleteOpts struct {
	// Force=true asks the server to skip the kind's PostDelete
	// reconciliation hook (e.g. provider teardown for Deployment) and
	// only soft-delete the row. Useful for orphaned records whose
	// external state is already gone or unreachable.
	Force bool
}

// Delete soft-deletes the (kind, namespace, name, version) row. Returns
// ErrNotFound when the row doesn't exist. See Store.Delete for the
// soft-delete semantics (the row stays visible with DeletionTimestamp
// set until the GC pass purges it).
func (c *Client) Delete(ctx context.Context, kind, namespace, name, version string, opts ...DeleteOpts) error {
	var force bool
	if len(opts) > 0 {
		force = opts[0].Force
	}
	q := namespaceQuery(namespace)
	if force {
		if q == "" {
			q = "?force=true"
		} else {
			q += "&force=true"
		}
	}
	path := fmt.Sprintf("/%s/%s/%s%s",
		v1alpha1.PluralFor(kind),
		url.PathEscape(name),
		url.PathEscape(version),
		q)
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
	DryRun bool
}

// Apply sends a multi-doc YAML body to POST /v0/apply and returns per-resource results.
// Returns an error only on request-level failures (network, 4xx from server).
// Per-resource errors are encoded in the returned results.
func (c *Client) Apply(ctx context.Context, body []byte, opts ApplyOpts) ([]arv0.ApplyResult, error) {
	return c.applyBatch(ctx, http.MethodPost, body, opts)
}

// DeleteViaApply sends a DELETE /v0/apply with a YAML body and returns per-resource results.
// Mirrors Apply but uses the DELETE HTTP method.
func (c *Client) DeleteViaApply(ctx context.Context, body []byte) ([]arv0.ApplyResult, error) {
	return c.applyBatch(ctx, http.MethodDelete, body, ApplyOpts{})
}

func (c *Client) applyBatch(ctx context.Context, method string, body []byte, opts ApplyOpts) ([]arv0.ApplyResult, error) {
	path := "/apply"
	q := url.Values{}
	if opts.DryRun {
		q.Set("dryRun", "true")
	}
	if enc := q.Encode(); enc != "" {
		path += "?" + enc
	}

	req, err := c.newRequestWithBody(method, path, bytes.NewReader(body), "application/yaml")
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)

	var out arv0.ApplyResultsResponse
	if err := c.doJSON(req, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// =============================================================================

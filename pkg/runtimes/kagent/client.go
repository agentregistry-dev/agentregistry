package kagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	kagentHTTPTimeout = 30 * time.Second
	defaultUserID     = "admin@kagent.dev"
)

// ErrAuthExpired reports a kagent 401; callers can map it to a retry hint.
var ErrAuthExpired = errors.New("kagent authentication expired")

var (
	errNotFound       = errors.New("kagent resource not found")
	errAlreadyExists  = errors.New("kagent resource already exists")
	toolDeleteTimeout = 30 * time.Second
	toolDeletePoll    = 500 * time.Millisecond
)

type remoteWorkload struct{ Kind, Namespace, Name string }

// TokenSource resolves the bearer token to send on each kagent request.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

type staticTokenSource string

func (s staticTokenSource) Token(context.Context) (string, error) { return string(s), nil }

func staticToken(token string) TokenSource { return staticTokenSource(token) }

type kagentClient interface {
	ensureAgent(ctx context.Context, agent *agentPayload) error
	deleteAgent(ctx context.Context, namespace, name string) error
	ensureToolServer(ctx context.Context, ts *toolServerSpec) error
	deleteToolServer(ctx context.Context, namespace, name string) error
	listAgents(ctx context.Context) ([]remoteWorkload, error)
	listToolServers(ctx context.Context) ([]remoteWorkload, error)
}

type clientFactory func(cfg runtimeConfig) (kagentClient, error)

type restClient struct {
	baseURL     string
	userID      string
	tokenSource TokenSource
	httpClient  *http.Client
}

type standardResponse[T any] struct {
	Error   bool   `json:"error"`
	Data    T      `json:"data"`
	Message string `json:"message"`
}

type agentResponse struct {
	Agent *agentPayload `json:"agent"`
}

type toolServerResponse struct {
	Ref string `json:"ref"`
}

type toolServerCreateRequest struct {
	Type            string                  `json:"type"`
	RemoteMCPServer *remoteMCPServerPayload `json:"remoteMCPServer,omitempty"`
	MCPServer       *mcpServerPayload       `json:"mcpServer,omitempty"`
}

func newKagentHTTPClient() *http.Client {
	return &http.Client{Timeout: kagentHTTPTimeout}
}

func newRESTClient(cfg runtimeConfig, tokenSource TokenSource) (kagentClient, error) {
	userID := cfg.Auth.UserID
	if userID == "" {
		userID = defaultUserID
	}
	return &restClient{
		baseURL:     strings.TrimRight(cfg.URL, "/"),
		userID:      userID,
		tokenSource: tokenSource,
		httpClient:  newKagentHTTPClient(),
	}, nil
}

func (c *restClient) ensureAgent(ctx context.Context, agent *agentPayload) error {
	body, err := json.Marshal(agent)
	if err != nil {
		return fmt.Errorf("marshal kagent agent: %w", err)
	}
	var response standardResponse[agentResponse]
	err = c.doJSON(ctx, http.MethodPost, "/api/agents", body, &response)
	if errors.Is(err, errAlreadyExists) {
		err = c.doJSON(ctx, http.MethodPut, "/api/agents", body, &response)
	}
	if err != nil {
		return fmt.Errorf("ensure kagent agent %s/%s: %w", agent.Namespace, agent.Name, err)
	}
	return responseError("ensure kagent agent", response.Error, response.Message)
}

func (c *restClient) deleteAgent(ctx context.Context, namespace, name string) error {
	path := fmt.Sprintf("/api/agents/%s/%s", namespace, name)
	var response standardResponse[json.RawMessage]
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return fmt.Errorf("delete kagent agent %s/%s: %w", namespace, name, err)
	}
	return responseError("delete kagent agent", response.Error, response.Message)
}

func (c *restClient) ensureToolServer(ctx context.Context, server *toolServerSpec) error {
	body, err := json.Marshal(toolServerCreateRequest{
		Type:            server.Kind,
		RemoteMCPServer: server.Remote,
		MCPServer:       server.MCP,
	})
	if err != nil {
		return fmt.Errorf("marshal kagent tool server: %w", err)
	}
	err = c.createToolServer(ctx, body)
	if err == nil {
		return nil
	}
	if !errors.Is(err, errAlreadyExists) {
		return err
	}

	namespace, name := server.Namespace(), server.Name()
	if err := c.deleteToolServer(ctx, namespace, name); err != nil {
		return fmt.Errorf("replace kagent tool server %s/%s: %w", namespace, name, err)
	}
	if err := c.waitToolServerGone(ctx, namespace, name); err != nil {
		return fmt.Errorf("replace kagent tool server %s/%s: %w", namespace, name, err)
	}
	if err := c.createToolServer(ctx, body); err != nil {
		return fmt.Errorf(
			"replace kagent tool server %s/%s after delete: %w", namespace, name, err,
		)
	}
	return nil
}

func (c *restClient) createToolServer(ctx context.Context, body json.RawMessage) error {
	var response standardResponse[toolServerResponse]
	if err := c.doJSON(ctx, http.MethodPost, "/api/toolservers", body, &response); err != nil {
		return fmt.Errorf("create kagent tool server: %w", err)
	}
	return responseError("create kagent tool server", response.Error, response.Message)
}

func (c *restClient) deleteToolServer(ctx context.Context, namespace, name string) error {
	path := fmt.Sprintf("/api/toolservers/%s/%s", namespace, name)
	var response standardResponse[json.RawMessage]
	if err := c.doJSON(ctx, http.MethodDelete, path, nil, &response); err != nil {
		return fmt.Errorf("delete kagent tool server %s/%s: %w", namespace, name, err)
	}
	return responseError("delete kagent tool server", response.Error, response.Message)
}

func (c *restClient) waitToolServerGone(ctx context.Context, namespace, name string) error {
	deadline, cancel := context.WithTimeout(ctx, toolDeleteTimeout)
	defer cancel()
	ticker := time.NewTicker(toolDeletePoll)
	defer ticker.Stop()
	var lastCheckError error

	for {
		exists, err := c.toolServerExists(deadline, namespace, name)
		if err == nil && !exists {
			return nil
		}
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			lastCheckError = err
		}
		select {
		case <-deadline.Done():
			if lastCheckError != nil {
				return fmt.Errorf(
					"tool server %s/%s deletion was not confirmed: %w",
					namespace,
					name,
					lastCheckError,
				)
			}
			return fmt.Errorf(
				"tool server %s/%s still present after %s", namespace, name, toolDeleteTimeout,
			)
		case <-ticker.C:
		}
	}
}

func (c *restClient) toolServerExists(
	ctx context.Context,
	namespace, name string,
) (bool, error) {
	servers, err := c.listToolServersRaw(ctx)
	if err != nil {
		return false, fmt.Errorf("check deleted kagent tool server: %w", err)
	}
	for _, server := range servers {
		serverNamespace, serverName := splitRef(server.Ref)
		if serverNamespace == namespace && serverName == name {
			return true, nil
		}
	}
	return false, nil
}

func (c *restClient) listAgents(ctx context.Context) ([]remoteWorkload, error) {
	agents, err := c.listAgentResponses(ctx)
	if err != nil {
		return nil, fmt.Errorf("list kagent agents: %w", err)
	}
	workloads := make([]remoteWorkload, 0, len(agents))
	for _, agent := range agents {
		if agent.Agent == nil || agent.Agent.Name == "" {
			continue
		}
		workloads = append(workloads, remoteWorkload{
			Kind:      v1alpha1.KindAgent,
			Namespace: agent.Agent.Namespace,
			Name:      agent.Agent.Name,
		})
	}
	return workloads, nil
}

func (c *restClient) listAgentResponses(ctx context.Context) ([]agentResponse, error) {
	var response standardResponse[[]agentResponse]
	if err := c.doJSON(ctx, http.MethodGet, "/api/agents", nil, &response); err != nil {
		return nil, err
	}
	if err := responseError("list kagent agents", response.Error, response.Message); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *restClient) listToolServers(ctx context.Context) ([]remoteWorkload, error) {
	servers, err := c.listToolServersRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("list kagent tool servers: %w", err)
	}
	workloads := make([]remoteWorkload, 0, len(servers))
	for _, server := range servers {
		namespace, name := splitRef(server.Ref)
		workloads = append(workloads, remoteWorkload{
			Kind:      v1alpha1.KindMCPServer,
			Namespace: namespace,
			Name:      name,
		})
	}
	return workloads, nil
}

func (c *restClient) listToolServersRaw(ctx context.Context) ([]toolServerResponse, error) {
	var response standardResponse[[]toolServerResponse]
	if err := c.doJSON(ctx, http.MethodGet, "/api/toolservers", nil, &response); err != nil {
		return nil, err
	}
	if err := responseError("list kagent tool servers", response.Error, response.Message); err != nil {
		return nil, err
	}
	return response.Data, nil
}

func (c *restClient) doJSON(
	ctx context.Context,
	method, path string,
	body json.RawMessage,
	response any,
) error {
	request, err := http.NewRequestWithContext(
		ctx,
		method,
		c.baseURL+path,
		bytes.NewReader(body),
	)
	if err != nil {
		return fmt.Errorf("build kagent request: %w", err)
	}
	request.Header.Set("X-User-Id", c.userID)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.tokenSource != nil {
		token, err := c.tokenSource.Token(ctx)
		if err != nil {
			return fmt.Errorf("resolve kagent token: %w", err)
		}
		if token == "" {
			return errors.New("resolve kagent token: empty token")
		}
		request.Header.Set("Authorization", "Bearer "+token)
	}

	httpResponse, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("kagent %s %s: %w", method, path, err)
	}
	defer httpResponse.Body.Close()
	responseBody, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return fmt.Errorf("read kagent response: %w", err)
	}
	if httpResponse.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("kagent %s %s: %w", method, path, ErrAuthExpired)
	}
	if httpResponse.StatusCode == http.StatusNotFound {
		return fmt.Errorf("kagent %s %s: %w", method, path, errNotFound)
	}
	if httpResponse.StatusCode == http.StatusInternalServerError &&
		strings.Contains(strings.ToLower(string(responseBody)), "already exists") {
		return fmt.Errorf("kagent %s %s: %w", method, path, errAlreadyExists)
	}
	if httpResponse.StatusCode < http.StatusOK || httpResponse.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf(
			"kagent %s %s returned %d: %s",
			method,
			path,
			httpResponse.StatusCode,
			strings.TrimSpace(string(responseBody)),
		)
	}
	if response != nil && len(bytes.TrimSpace(responseBody)) > 0 {
		if err := json.Unmarshal(responseBody, response); err != nil {
			return fmt.Errorf("decode kagent response: %w", err)
		}
	}
	return nil
}

func responseError(operation string, failed bool, message string) error {
	if !failed {
		return nil
	}
	return fmt.Errorf("%s: kagent returned error: %s", operation, message)
}

func splitRef(ref string) (namespace, name string) {
	if namespace, name, ok := strings.Cut(ref, "/"); ok {
		return namespace, name
	}
	return "", ref
}

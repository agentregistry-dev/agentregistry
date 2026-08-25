// Package microsoft discovers agents from Microsoft runtime APIs.
package microsoft

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	"github.com/agentregistry-dev/agentregistry/pkg/status"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

const (
	foundryScope         = "https://ai.azure.com/.default"
	foundryPreviewHeader = "Foundry-Features"
	foundryPreviewValue  = "HostedAgents=V1Preview"
)

type source struct {
	typ     string
	secrets secret.Resolver
	client  *http.Client
}

type agent struct {
	id, name, version, description, state, kind, model string
	tools                                              any
	raw                                                json.RawMessage
}

// NewFoundrySource returns the OSS Foundry discovery source.
func NewFoundrySource(resolver secret.Resolver) types.DeploymentDiscoverySource {
	return newSource(v1alpha1.TypeMicrosoftFoundry, resolver)
}

// NewCopilotStudioSource returns the OSS Copilot Studio discovery source.
func NewCopilotStudioSource(resolver secret.Resolver) types.DeploymentDiscoverySource {
	return newSource(v1alpha1.TypeMicrosoftCopilotStudio, resolver)
}

func newSource(typ string, resolver secret.Resolver) *source {
	return &source{typ: typ, secrets: resolver, client: &http.Client{Timeout: 30 * time.Second}}
}

func (s *source) Discover(ctx context.Context, in types.DiscoverInput) ([]types.DiscoveryResult, error) {
	if in.Runtime == nil || in.Runtime.Spec.Type != s.typ {
		return nil, fmt.Errorf("runtime of type %q is required", s.typ)
	}
	if s.typ == v1alpha1.TypeMicrosoftFoundry {
		return s.foundry(ctx, in.Runtime)
	}
	return s.copilot(ctx, in.Runtime)
}

func (s *source) foundry(ctx context.Context, runtime *v1alpha1.Runtime) ([]types.DiscoveryResult, error) {
	var cfg v1alpha1.MicrosoftFoundryRuntimeConfig
	if err := decode(runtime.Spec.Config, &cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.ProjectEndpoint) == "" {
		return nil, fmt.Errorf("projectEndpoint is required")
	}
	token, err := s.token(ctx, runtime, cfg.Auth, foundryScope)
	if err != nil {
		return nil, err
	}
	agents, err := s.foundryAgents(ctx, cfg.ProjectEndpoint, token)
	if err != nil {
		return nil, err
	}
	return results(agents, "foundry", map[string]string{
		status.MicrosoftFoundryProjectEndpointKey: cfg.ProjectEndpoint,
		status.MicrosoftFoundrySubscriptionIDKey:  cfg.SubscriptionID,
		status.MicrosoftFoundryResourceGroupKey:   cfg.ResourceGroup,
		status.MicrosoftFoundryTenantIDKey:        tenant(cfg.Auth.OIDC),
	}), nil
}

func (s *source) copilot(ctx context.Context, runtime *v1alpha1.Runtime) ([]types.DiscoveryResult, error) {
	var cfg v1alpha1.MicrosoftCopilotStudioRuntimeConfig
	if err := decode(runtime.Spec.Config, &cfg); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.DataEndpoint) == "" || strings.TrimSpace(cfg.EnvironmentID) == "" {
		return nil, fmt.Errorf("dataEndpoint and environmentId are required")
	}
	token, err := s.token(ctx, runtime, cfg.Auth, strings.TrimRight(cfg.DataEndpoint, "/")+"/.default")
	if err != nil {
		return nil, err
	}
	agents, err := s.copilotAgents(ctx, cfg.DataEndpoint, token)
	if err != nil {
		return nil, err
	}
	return results(agents, "copilot_studio", map[string]string{
		"dataEndpoint": cfg.DataEndpoint, "environmentId": cfg.EnvironmentID,
		"tenantId": tenant(cfg.Auth.OIDC),
	}), nil
}

func (s *source) token(ctx context.Context, runtime *v1alpha1.Runtime, auth v1alpha1.MicrosoftRuntimeAuth, scope string) (string, error) {
	if auth.OIDC == nil || auth.OIDC.ClientSecretRef == nil || s.secrets == nil {
		return "", fmt.Errorf("auth.oidc.clientSecretRef and secret resolver are required")
	}
	ref := *auth.OIDC.ClientSecretRef
	if ref.Namespace == "" {
		ref.Namespace = runtime.Metadata.NamespaceOrDefault()
	}
	secretValue, err := s.secrets.Resolve(ctx, v1alpha1.SecretRef(ref))
	if err != nil {
		return "", fmt.Errorf("resolve Microsoft client secret: %w", err)
	}
	endpoint, err := tokenURL(auth.OIDC.Issuer)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"client_credentials"}, "client_id": {auth.OIDC.ClientID}, "client_secret": {string(secretValue.Reveal())}, "scope": {scope}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := s.request(req)
	if err != nil {
		return "", fmt.Errorf("request Microsoft token: %w", err)
	}
	var response struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("decode Microsoft token response: %w", err)
	}
	if response.AccessToken == "" {
		return "", fmt.Errorf("microsoft token response contains no access token")
	}
	return response.AccessToken, nil
}

func (s *source) foundryAgents(ctx context.Context, endpoint, token string) ([]agent, error) {
	base := strings.TrimRight(endpoint, "/") + "/agents?api-version=v1"
	next := base
	var out []agent
	for next != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, next, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set(foundryPreviewHeader, foundryPreviewValue)
		body, err := s.request(req)
		if err != nil {
			return nil, fmt.Errorf("list Foundry agents: %w", err)
		}
		var page struct {
			Data    []json.RawMessage `json:"data"`
			HasMore bool              `json:"has_more"`
			LastID  string            `json:"last_id"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, raw := range page.Data {
			a, err := parseFoundry(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		if page.HasMore && page.LastID != "" {
			next = base + "&after=" + url.QueryEscape(page.LastID)
		} else {
			next = ""
		}
	}
	return out, nil
}

func (s *source) copilotAgents(ctx context.Context, endpoint, token string) ([]agent, error) {
	next := strings.TrimRight(endpoint, "/") + "/api/data/v9.2/bots?$select=botid,name,statecode,modifiedon,publishedon"
	var out []agent
	for next != "" {
		body, err := s.get(ctx, next, token)
		if err != nil {
			return nil, fmt.Errorf("list Copilot Studio agents: %w", err)
		}
		var page struct {
			Value []json.RawMessage `json:"value"`
			Next  string            `json:"@odata.nextLink"`
		}
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		for _, raw := range page.Value {
			a, err := parseCopilot(raw)
			if err != nil {
				return nil, err
			}
			out = append(out, a)
		}
		next = page.Next
	}
	return out, nil
}

func (s *source) get(ctx context.Context, endpoint, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	return s.request(req)
}

func (s *source) request(req *http.Request) ([]byte, error) {
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API returned %d: %s", resp.StatusCode, truncate(string(body), 256))
	}
	return body, nil
}

func parseFoundry(raw json.RawMessage) (agent, error) {
	var value struct {
		ID, Name string
		Versions struct {
			Latest *struct {
				Version, Description, Status string
				Definition                   struct {
					Kind, Model string
					Tools       any
				}
			}
		}
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return agent{}, err
	}
	a := agent{id: value.ID, name: value.Name, raw: raw}
	if v := value.Versions.Latest; v != nil {
		a.version, a.description, a.state, a.kind, a.model, a.tools = v.Version, v.Description, v.Status, v.Definition.Kind, v.Definition.Model, v.Definition.Tools
	}
	return a, nil
}

func parseCopilot(raw json.RawMessage) (agent, error) {
	var value struct {
		ID    string `json:"botid"`
		Name  string `json:"name"`
		State int    `json:"statecode"`
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return agent{}, err
	}
	state := "inactive"
	if value.State == 0 {
		state = "active"
	}
	return agent{id: value.ID, name: value.Name, state: state, raw: raw}, nil
}

func results(agents []agent, platform string, common map[string]string) []types.DiscoveryResult {
	seen := map[string]bool{}
	out := make([]types.DiscoveryResult, 0, len(agents))
	now := time.Now().UTC().Format(time.RFC3339)
	for _, a := range agents {
		name := strings.TrimSpace(a.name)
		if name == "" {
			name = strings.TrimSpace(a.id)
		}
		if name == "" {
			continue
		}
		key := a.id
		if key == "" {
			key = a.name + "\x00" + a.version
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		metadata := map[string]string{
			status.MicrosoftFoundryProviderKey:   "microsoft",
			status.MicrosoftFoundryPlatformKey:   platform,
			status.MicrosoftFoundryLastSeenAtKey: now,
		}
		for k, v := range common {
			put(metadata, k, v)
		}
		put(metadata, status.RuntimeMetadataRemoteIDKey, a.id)
		put(metadata, status.MicrosoftFoundryRemoteNameKey, a.name)
		put(metadata, status.MicrosoftFoundryRemoteVersionKey, a.version)
		put(metadata, status.MicrosoftFoundryDescriptionKey, a.description)
		put(metadata, status.MicrosoftFoundryStateKey, a.state)
		put(metadata, status.MicrosoftFoundryKindKey, a.kind)
		put(metadata, status.MicrosoftFoundryModelKey, a.model)
		put(metadata, status.MicrosoftFoundryToolsKey, tools(a.tools))
		put(metadata, status.MicrosoftFoundryAgentGUIDKey, guid(a.raw))
		out = append(out, types.DiscoveryResult{TargetKind: v1alpha1.KindAgent, Name: name, RuntimeMetadata: metadata})
	}
	return out
}

func decode(config map[string]any, out any) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}
func tokenURL(issuer string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(issuer))
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("auth.oidc.issuer must be a URL")
	}
	u.Path = "/" + strings.TrimSuffix(strings.Trim(u.Path, "/"), "/v2.0") + "/oauth2/v2.0/token"
	u.RawQuery, u.Fragment = "", ""
	return u.String(), nil
}
func tenant(oidc *v1alpha1.RuntimeOIDCAuth) string {
	if oidc == nil {
		return ""
	}
	u, err := url.Parse(oidc.Issuer)
	if err != nil {
		return ""
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 {
		return ""
	}
	return parts[0]
}
func put(values map[string]string, key, value string) {
	if value = strings.TrimSpace(value); value != "" {
		values[key] = value
	}
}
func tools(value any) string {
	raw, _ := json.Marshal(value)
	var list []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &list) != nil {
		return ""
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		if item.Type != "" {
			out = append(out, item.Type)
		}
	}
	return strings.Join(out, ", ")
}
func guid(raw json.RawMessage) string {
	var value struct {
		Versions struct {
			Latest struct {
				GUID string `json:"agent_guid"`
			} `json:"latest"`
		} `json:"versions"`
	}
	if json.Unmarshal(raw, &value) != nil {
		return ""
	}
	return value.Versions.Latest.GUID
}
func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "..."
}

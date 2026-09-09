package microsoft

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

type memoryStore struct{ data map[string]map[string][]byte }

func (s *memoryStore) Put(context.Context, string, string, map[string][]byte) error { return nil }
func (s *memoryStore) Delete(context.Context, string, string) error                 { return nil }
func (s *memoryStore) Get(_ context.Context, namespace, name string) (map[string][]byte, error) {
	return s.data[namespace+"/"+name], nil
}

func TestDiscoverySources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		runtimeType string
		apiPath     string
		apiResponse any
		config      func(string) map[string]any
		wantScope   string
		want        types.DiscoveryResult
	}{
		{
			name:        "foundry",
			runtimeType: v1alpha1.TypeMicrosoftFoundry,
			apiPath:     "/project/agents",
			apiResponse: map[string]any{"data": []any{map[string]any{
				"id": "agent-1", "name": "weather", "versions": map[string]any{"latest": map[string]any{
					"version": "3", "description": "Weather agent", "status": "active", "agent_guid": "guid-1",
					"definition": map[string]any{"kind": "prompt", "model": "gpt", "tools": []any{map[string]any{"type": "function"}}},
				}},
			}}},
			config: func(base string) map[string]any {
				return map[string]any{
					"projectEndpoint": base + "/project", "subscriptionId": "sub-1", "resourceGroup": "rg-1",
					"auth": authConfig(base),
				}
			},
			wantScope: foundryScope,
			want: types.DiscoveryResult{TargetKind: v1alpha1.KindAgent, Name: "weather", InternalMeta: types.DeploymentInternalMeta{
				RuntimeID: "agent-1", RuntimeName: "weather", RuntimeResourceID: "agent-1",
			}},
		},
		{
			name:        "copilot studio",
			runtimeType: v1alpha1.TypeMicrosoftCopilotStudio,
			apiPath:     "/dataverse/api/data/v9.2/bots",
			apiResponse: map[string]any{"value": []any{map[string]any{"botid": "bot-1", "name": "support", "statecode": 0}}},
			config: func(base string) map[string]any {
				return map[string]any{"environmentId": "env-1", "dataEndpoint": base + "/dataverse", "auth": authConfig(base)}
			},
			wantScope: "DYNAMIC",
			want: types.DiscoveryResult{TargetKind: v1alpha1.KindAgent, Name: "support", InternalMeta: types.DeploymentInternalMeta{
				RuntimeID: "bot-1", RuntimeName: "support", RuntimeResourceID: "bot-1",
			}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var tokenScope string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/tenant/oauth2/v2.0/token":
					assert.NoError(t, r.ParseForm())
					tokenScope = r.Form.Get("scope")
					assert.Equal(t, "client-secret", r.Form.Get("client_secret"))
					_, _ = w.Write([]byte(`{"access_token":"token"}`))
				case tt.apiPath:
					assert.Equal(t, "Bearer token", r.Header.Get("Authorization"))
					if tt.runtimeType == v1alpha1.TypeMicrosoftFoundry {
						assert.Equal(t, foundryPreviewValue, r.Header.Get(foundryPreviewHeader))
					}
					assert.NoError(t, json.NewEncoder(w).Encode(tt.apiResponse))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			service := secret.NewService(&memoryStore{data: map[string]map[string][]byte{
				"default/credentials": {"clientSecret": []byte("client-secret")},
			}})
			s := newSource(tt.runtimeType, service)
			runtime := &v1alpha1.Runtime{Metadata: v1alpha1.ObjectMeta{Name: "runtime"}, Spec: v1alpha1.RuntimeSpec{Type: tt.runtimeType, Config: tt.config(server.URL)}}
			got, err := s.Discover(context.Background(), types.DiscoverInput{Runtime: runtime})
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.Equal(t, tt.want, got[0])
			if tt.wantScope == "DYNAMIC" {
				require.Equal(t, server.URL+"/dataverse/.default", tokenScope)
			} else {
				require.Equal(t, tt.wantScope, tokenScope)
			}
		})
	}
}

func TestDiscoveryPagination(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		run  func(context.Context, *source, string) ([]agent, error)
		h    http.HandlerFunc
	}{
		{
			name: "foundry cursor",
			run: func(ctx context.Context, s *source, endpoint string) ([]agent, error) {
				return s.foundryAgents(ctx, endpoint, "token")
			},
			h: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("after") == "agent-1" {
					_, _ = w.Write([]byte(`{"data":[{"id":"agent-2","name":"two"}]}`))
					return
				}
				_, _ = w.Write([]byte(`{"data":[{"id":"agent-1","name":"one"}],"has_more":true,"last_id":"agent-1"}`))
			},
		},
		{
			name: "copilot next link",
			run: func(ctx context.Context, s *source, endpoint string) ([]agent, error) {
				return s.copilotAgents(ctx, endpoint, "token")
			},
			h: func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Query().Get("page") == "2" {
					_, _ = w.Write([]byte(`{"value":[{"botid":"bot-2","name":"two"}]}`))
					return
				}
				next := "http://" + r.Host + r.URL.Path + "?page=2"
				_ = json.NewEncoder(w).Encode(map[string]any{"value": []any{map[string]any{"botid": "bot-1", "name": "one"}}, "@odata.nextLink": next})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			server := httptest.NewServer(tt.h)
			defer server.Close()
			got, err := tt.run(context.Background(), newSource("test", nil), server.URL)
			require.NoError(t, err)
			require.Len(t, got, 2)
			require.NotEqual(t, got[0].id, got[1].id)
		})
	}
}

func TestDiscoveryHTTPError(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "denied", http.StatusForbidden)
	}))
	t.Cleanup(server.Close)

	tests := []struct {
		name string
		run  func(*source) error
	}{
		{name: "foundry", run: func(s *source) error {
			_, err := s.foundryAgents(context.Background(), server.URL, "token")
			return err
		}},
		{name: "copilot", run: func(s *source) error {
			_, err := s.copilotAgents(context.Background(), server.URL, "token")
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.ErrorContains(t, tt.run(newSource("test", nil)), "403")
		})
	}
}

func TestDiscoveryResultsDeduplicate(t *testing.T) {
	t.Parallel()
	got := results([]agent{
		{id: "same", name: "one"},
		{id: "same", name: "duplicate"},
		{name: "two", version: "1"},
		{name: "two", version: "1"},
	}, "foundry", nil)
	require.Len(t, got, 2)
	require.Equal(t, "same", got[0].InternalMeta.RuntimeID)
	require.Empty(t, got[1].InternalMeta.RuntimeID)
}

func TestTokenURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		issuer  string
		want    string
		wantErr string
	}{
		{
			name:   "tenant issuer",
			issuer: "https://login.microsoftonline.com/tenant/v2.0",
			want:   "https://login.microsoftonline.com/tenant/oauth2/v2.0/token",
		},
		{
			name:    "missing tenant path",
			issuer:  "https://login.microsoftonline.com",
			wantErr: "must include a tenant path",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := tokenURL(tt.issuer)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}

func authConfig(base string) map[string]any {
	return map[string]any{"oidc": map[string]any{
		"issuer": base + "/tenant/v2.0", "clientId": "client", "clientSecretRef": map[string]any{"name": "credentials", "key": "clientSecret"},
	}}
}

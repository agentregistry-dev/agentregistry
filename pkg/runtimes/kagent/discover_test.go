package kagent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

func TestDiscover(t *testing.T) {
	fc := newFakeClient()
	agent := &agentPayload{}
	agent.Name, agent.Namespace = "found-agent", "kagent"
	require.NoError(t, fc.ensureAgent(context.Background(), agent))

	a := testAdapter(fc).(types.DeploymentDiscoverySource)
	got, err := a.Discover(context.Background(), types.DiscoverInput{
		Runtime: &v1alpha1.Runtime{
			Metadata: v1alpha1.ObjectMeta{Name: "my-kagent"},
			Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
				"kagentUrl": "http://kagent:8083", "namespace": "kagent",
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "found-agent",
		RuntimeMetadata: map[string]string{
			types.RuntimeMetadataRemoteIDKey: "found-agent",
			"namespace":                      "kagent",
		},
	}}, got)
}

func TestDiscoverDefaultsRuntimeNamespaceToKagent(t *testing.T) {
	fc := newFakeClient()
	agent := &agentPayload{}
	agent.Name, agent.Namespace = "found-agent", "kagent"
	require.NoError(t, fc.ensureAgent(context.Background(), agent))

	a := testAdapter(fc).(types.DeploymentDiscoverySource)
	got, err := a.Discover(context.Background(), types.DiscoverInput{
		Runtime: &v1alpha1.Runtime{
			Metadata: v1alpha1.ObjectMeta{Name: "my-kagent"},
			Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
				"kagentUrl": "http://kagent:8083",
			}},
		},
	})
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "found-agent", got[0].Name)
}

// TestDiscoverSurvivesRealAgentIDFormat drives Discover against a real
// restClient (not fakeClient) with the live kagent__NS__name response shape,
// proving the namespace filter no longer drops every agent.
func TestDiscoverSurvivesRealAgentIDFormat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/agents":
			w.Write([]byte(`{"error":false,"data":[{"id":"kagent__NS__smoke_byo_agent","agent":{"apiVersion":"kagent.dev/v1alpha2","kind":"Agent","metadata":{"name":"smoke-byo-agent","namespace":"kagent"}}}]}`))
		case "/api/toolservers":
			w.Write([]byte(`{"error":false,"data":[]}`))
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	a := New(withClientFactory(func(cfg runtimeConfig) (kagentClient, error) { return newRESTClient(cfg, nil) })).(types.DeploymentDiscoverySource)
	got, err := a.Discover(context.Background(), types.DiscoverInput{
		Runtime: &v1alpha1.Runtime{
			Metadata: v1alpha1.ObjectMeta{Name: "my-kagent"},
			Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
				"kagentUrl": srv.URL, "namespace": "kagent",
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "smoke-byo-agent",
		RuntimeMetadata: map[string]string{
			types.RuntimeMetadataRemoteIDKey: "smoke-byo-agent",
			"namespace":                      "kagent",
		},
	}}, got)
}

func TestDiscoverFiltersToConfiguredNamespace(t *testing.T) {
	fc := newFakeClient()
	inNS := &agentPayload{}
	inNS.Name, inNS.Namespace = "in-ns-agent", "kagent"
	require.NoError(t, fc.ensureAgent(context.Background(), inNS))
	otherNS := &agentPayload{}
	otherNS.Name, otherNS.Namespace = "other-ns-agent", "other"
	require.NoError(t, fc.ensureAgent(context.Background(), otherNS))

	a := testAdapter(fc).(types.DeploymentDiscoverySource)
	got, err := a.Discover(context.Background(), types.DiscoverInput{
		Runtime: &v1alpha1.Runtime{
			Metadata: v1alpha1.ObjectMeta{Name: "my-kagent"},
			Spec: v1alpha1.RuntimeSpec{Type: RuntimeType, Config: map[string]any{
				"kagentUrl": "http://kagent:8083", "namespace": "kagent",
			}},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, []types.DiscoveryResult{{
		TargetKind: v1alpha1.KindAgent,
		Name:       "in-ns-agent",
		RuntimeMetadata: map[string]string{
			types.RuntimeMetadataRemoteIDKey: "in-ns-agent",
			"namespace":                      "kagent",
		},
	}}, got)
}

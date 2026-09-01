package kagent

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestAuthTransportSetsHeaders(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"error":false,"data":[]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL, Auth: authConfig{UserID: "ar"}}, StaticToken("tok"))
	require.NoError(t, err)
	_, err = c.listAgents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "Bearer tok", got.Get("Authorization"))
	assert.Equal(t, "ar", got.Get("X-User-ID"))
}

func TestAuthTransportRejectsEmptyToken(t *testing.T) {
	requestSent := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestSent = true
		w.Write([]byte(`{"error":false,"data":[]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, StaticToken(""))
	require.NoError(t, err)
	_, err = c.listAgents(context.Background())
	require.ErrorContains(t, err, "resolve kagent token: empty token")
	assert.False(t, requestSent)
}

func TestUnauthorizedIsAuthExpired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	_, err = c.listAgents(context.Background())
	assert.ErrorIs(t, err, ErrAuthExpired)
}

func TestIsNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	err = c.deleteAgent(context.Background(), "ns", "missing")
	assert.ErrorIs(t, err, errNotFound)
}

func TestAuthTransportDefaultsUserID(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.Write([]byte(`{"error":false,"data":[]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	_, err = c.listAgents(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "admin@kagent.dev", got.Get("X-User-ID"))
}

func TestEnsureAgentConflictFallsBackToUpdate(t *testing.T) {
	var updateCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"Failed to create Agent in Kubernetes: agents.kagent.dev \"foo\" already exists"}`))
		case http.MethodPut:
			updateCalled = true
			w.Write([]byte(`{"error":false,"data":{}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	agent := &agentPayload{}
	agent.Namespace, agent.Name = "ns", "foo"
	require.NoError(t, c.ensureAgent(context.Background(), agent))
	assert.True(t, updateCalled, "expected update fallback on already-exists conflict")
}

func TestEnsureAgentOtherErrorNotSwallowed(t *testing.T) {
	var updateCalled bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"boom"}`))
		case http.MethodPut:
			updateCalled = true
			w.Write([]byte(`{"error":false,"data":{}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	agent := &agentPayload{}
	agent.Namespace, agent.Name = "ns", "foo"
	err = c.ensureAgent(context.Background(), agent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
	assert.False(t, updateCalled, "must not fall back to update on a non-conflict create error")
}

func TestEnsureAgentPostsExpectedPayload(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		gotBody = string(body)
		w.Write([]byte(`{"error":false}`))
	}))
	defer srv.Close()

	in := byoApplyInput()
	agent, err := buildBYOAgent(
		context.Background(),
		in,
		runtimeConfig{URL: "https://kagent.example.com", Namespace: "kagent"},
		nil,
	)
	require.NoError(t, err)
	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	require.NoError(t, c.ensureAgent(context.Background(), agent))

	assert.JSONEq(t, `{
		"metadata": {"name": "my-agent", "namespace": "kagent"},
		"spec": {
			"type": "BYO",
			"byo": {"deployment": {
				"image": "ghcr.io/acme/agent:1.0.0",
				"env": [
					{"name": "FOO", "value": "bar"},
					{"name": "HOST", "value": "0.0.0.0"},
					{"name": "KAGENT_NAMESPACE", "value": "kagent"},
					{"name": "KAGENT_NAME", "value": "My Agent"},
					{"name": "KAGENT_URL", "value": "https://kagent.example.com"}
				]
			}},
			"description": "my-agent"
		},
		"status": {}
	}`, gotBody)
}

func testRemoteToolServerSpec(namespace, name string) *toolServerSpec {
	return &toolServerSpec{
		Kind: "RemoteMCPServer",
		Remote: &remoteMCPServerPayload{
			ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name},
			Spec:       remoteMCPServerPayloadSpec{URL: "https://mcp.example.com"},
		},
	}
}

func TestEnsureToolServerAlreadyExistsIsReplaced(t *testing.T) {
	requests := []string{}
	createCalls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests = append(requests, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPost:
			createCalls++
			if createCalls == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte(`{"error":"Failed to create RemoteMCPServer in Kubernetes: remotemcpservers.kagent.dev \"foo\" already exists"}`))
				return
			}
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"error":false,"data":{}}`))
		case http.MethodDelete:
			w.Write([]byte(`{"error":false,"data":{}}`))
		case http.MethodGet:
			w.Write([]byte(`{"error":false,"data":[]}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	require.NoError(t, c.ensureToolServer(context.Background(), testRemoteToolServerSpec("ns", "foo")))
	assert.Equal(t, []string{
		"POST /api/toolservers",
		"DELETE /api/toolservers/ns/foo",
		"GET /api/toolservers",
		"POST /api/toolservers",
	}, requests)
}

func TestEnsureToolServerReplacementReportsLastCheckError(t *testing.T) {
	oldTimeout, oldPoll := toolDeleteTimeout, toolDeletePoll
	toolDeleteTimeout, toolDeletePoll = 40*time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() {
		toolDeleteTimeout, toolDeletePoll = oldTimeout, oldPoll
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"resource already exists"}`))
		case http.MethodDelete:
			w.Write([]byte(`{"error":false,"data":{}}`))
		case http.MethodGet:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	client, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	err = client.ensureToolServer(
		context.Background(),
		testRemoteToolServerSpec("ns", "foo"),
	)
	require.ErrorContains(t, err, "deletion was not confirmed")
	require.ErrorContains(t, err, "unavailable")
}

func TestEnsureToolServerOtherErrorNotSwallowed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"boom"}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	err = c.ensureToolServer(context.Background(), testRemoteToolServerSpec("ns", "foo"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}

func TestEnsureRemoteToolServerPostsExpectedPayload(t *testing.T) {
	var gotPath, gotUserID, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUserID = r.Header.Get("X-User-ID")
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"error":false,"data":{}}`))
	}))
	defer srv.Close()

	in := byoApplyInput()
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "remote-tools", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{Remote: &v1alpha1.MCPRemote{
			URL: "https://mcp.example.com/mcp",
		}},
	}
	server, err := buildToolServer(in, runtimeConfig{Namespace: "kagent"}, deployConfig{})
	require.NoError(t, err)
	c, err := newRESTClient(runtimeConfig{URL: srv.URL, Auth: authConfig{UserID: "ar"}}, nil)
	require.NoError(t, err)
	require.NoError(t, c.ensureToolServer(context.Background(), server))

	assert.Equal(t, "/api/toolservers", gotPath)
	assert.Equal(t, "ar", gotUserID)
	assert.JSONEq(t, `{
		"type": "RemoteMCPServer",
		"remoteMCPServer": {
			"metadata": {"name": "remote-tools", "namespace": "kagent"},
			"spec": {
				"description": "remote-tools",
				"protocol": "STREAMABLE_HTTP",
				"url": "https://mcp.example.com/mcp"
			},
			"status": {}
		}
	}`, gotBody)
}

func TestEnsureSourceToolServerPostsExpectedPayload(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		gotBody = string(body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"error":false,"data":{}}`))
	}))
	defer srv.Close()

	in := byoApplyInput()
	in.Deployment.Spec.Env = nil
	in.Target = &v1alpha1.MCPServer{
		Metadata: v1alpha1.ObjectMeta{Name: "source-tools", Namespace: "default"},
		Spec: v1alpha1.MCPServerSpec{Source: &v1alpha1.MCPServerSource{Package: &v1alpha1.MCPPackage{
			Origin: v1alpha1.MCPPackageOrigin{
				Type:       v1alpha1.MCPPackageOriginTypeNPM,
				Identifier: "@acme/source-tools",
				NPM:        &v1alpha1.MCPPackageOriginNPM{Version: "1.2.3"},
			},
			Transport: v1alpha1.MCPTransport{Type: "stdio"},
		}}},
	}
	server, err := buildToolServer(in, runtimeConfig{Namespace: "kagent"}, deployConfig{})
	require.NoError(t, err)
	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	require.NoError(t, c.ensureToolServer(context.Background(), server))

	assert.JSONEq(t, `{
		"type": "MCPServer",
		"mcpServer": {
			"metadata": {"name": "source-tools", "namespace": "kagent"},
			"spec": {
				"deployment": {
					"image": "node:24-alpine3.21",
					"cmd": "npx",
					"args": ["-y", "@acme/source-tools@1.2.3"]
				},
				"transportType": "stdio",
				"stdioTransport": {}
			},
			"status": {}
		}
	}`, gotBody)
}

func TestListToolServersDecodesWrappedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/toolservers", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Write([]byte(`{"error":false,"data":[{"ref":"kagent/x","discoveredTools":[{}]}]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	workloads, err := c.listToolServers(context.Background())
	require.NoError(t, err)
	require.Len(t, workloads, 1)
	assert.Equal(t, remoteWorkload{Kind: "MCPServer", Namespace: "kagent", Name: "x"}, workloads[0])
}

func TestListToolServersRejectsErrorEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":true,"message":"backend unavailable"}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	_, err = c.listToolServers(context.Background())
	require.ErrorContains(t, err, "backend unavailable")
}

func TestListAgentsReadsIdentityFromAgentMetadataNotID(t *testing.T) {
	// Kagent v0.9.12 and v0.10.0-rc3 use "kagent__NS__name" for
	// AgentResponse.ID, not
	// "ns/name"; the
	// nested agent object's metadata is the only reliable source of identity.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/agents", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Write([]byte(`{"error":false,"data":[{"id":"kagent__NS__smoke_byo_agent","agent":{"apiVersion":"kagent.dev/v1alpha2","kind":"Agent","metadata":{"name":"smoke-byo-agent","namespace":"kagent"}}}]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	workloads, err := c.listAgents(context.Background())
	require.NoError(t, err)
	require.Len(t, workloads, 1)
	assert.Equal(t, remoteWorkload{Kind: v1alpha1.KindAgent, Namespace: "kagent", Name: "smoke-byo-agent"}, workloads[0])
}

func TestListAgentsSkipsEntriesMissingAgentObject(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"error":false,"data":[{"id":"kagent__NS__orphan"},{"id":"kagent__NS__ok","agent":{"metadata":{"name":"ok","namespace":"kagent"}}}]}`))
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL}, nil)
	require.NoError(t, err)
	workloads, err := c.listAgents(context.Background())
	require.NoError(t, err)
	require.Len(t, workloads, 1)
	assert.Equal(t, remoteWorkload{Kind: v1alpha1.KindAgent, Namespace: "kagent", Name: "ok"}, workloads[0])
}

func TestUpdateAgentPutsFlatAgentsPath(t *testing.T) {
	var putPath string
	var putUserID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"error":"Failed to create Agent in Kubernetes: agents.kagent.dev \"foo\" already exists"}`))
		case http.MethodPut:
			if r.URL.Path != "/api/agents" {
				http.Error(w, "not found", http.StatusMethodNotAllowed)
				return
			}
			putPath = r.URL.Path
			putUserID = r.Header.Get("X-User-ID")
			w.Write([]byte(`{"error":false,"data":{}}`))
		default:
			t.Fatalf("unexpected method %s", r.Method)
		}
	}))
	defer srv.Close()

	c, err := newRESTClient(runtimeConfig{URL: srv.URL, Auth: authConfig{UserID: "ar"}}, nil)
	require.NoError(t, err)
	agent := &agentPayload{}
	agent.Namespace, agent.Name = "ns", "foo"
	require.NoError(t, c.ensureAgent(context.Background(), agent))
	assert.Equal(t, "/api/agents", putPath)
	assert.Equal(t, "ar", putUserID)
}

func TestNewKagentHTTPClientSetsTimeout(t *testing.T) {
	httpClient := newKagentHTTPClient()
	assert.Equal(t, kagentHTTPTimeout, httpClient.Timeout)
}

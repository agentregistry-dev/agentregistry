package registryserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// A populated status.details is the trigger for the RawMessage schema
// bug: default inference described json.RawMessage as a byte array, so
// the JSON object it marshals to failed the SDK's output validation and
// every list/get on such an envelope errored out.
func TestOutputSchemaFor_AcceptsPopulatedRawMessage(t *testing.T) {
	d := &v1alpha1.Deployment{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "test-deployment"},
	}
	require.NoError(t, d.Status.SetDetailsKey("runtimeMetadata", map[string]any{"state": "ready"}))

	cases := map[string]struct {
		schema *jsonschema.Schema
		value  any
	}{
		"list": {
			schema: outputSchemaFor[listOutput[*v1alpha1.Deployment]](),
			value:  listOutput[*v1alpha1.Deployment]{Items: []*v1alpha1.Deployment{d}, Count: 1},
		},
		"get": {
			schema: outputSchemaFor[*v1alpha1.Deployment](),
			value:  d,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			// The SDK requires tool schemas to be objects at the top
			// level, so pointer outputs must be dereferenced.
			require.Equal(t, "object", tc.schema.Type)

			resolved, err := tc.schema.Resolve(nil)
			require.NoError(t, err)
			raw, err := json.Marshal(tc.value)
			require.NoError(t, err)
			var decoded any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.NoError(t, resolved.Validate(decoded))
		})
	}
}

func TestOutputSchemaFor_AcceptsPluginCommands(t *testing.T) {
	p := &v1alpha1.Plugin{
		Metadata: v1alpha1.ObjectMeta{Namespace: "default", Name: "test-plugin"},
		Status: v1alpha1.PluginStatus{
			Manifest: &v1alpha1.PluginManifest{
				Commands: &v1alpha1.CommandsField{Map: map[string]v1alpha1.CommandEntry{
					"docs": {Description: "Search documentation"},
					"page": {Description: "Open a page"},
				}},
			},
		},
	}

	cases := map[string]struct {
		schema *jsonschema.Schema
		value  any
	}{
		"list": {
			schema: outputSchemaFor[listOutput[*v1alpha1.Plugin]](),
			value:  listOutput[*v1alpha1.Plugin]{Items: []*v1alpha1.Plugin{p}, Count: 1},
		},
		"get": {
			schema: outputSchemaFor[*v1alpha1.Plugin](),
			value:  p,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			resolved, err := tc.schema.Resolve(nil)
			require.NoError(t, err)
			raw, err := json.Marshal(tc.value)
			require.NoError(t, err)
			var decoded any
			require.NoError(t, json.Unmarshal(raw, &decoded))
			require.NoError(t, resolved.Validate(decoded))
		})
	}
}

func TestMutableToolSchemasOmitTag(t *testing.T) {
	ctx := context.Background()
	stores := map[string]*v1alpha1store.Store{
		v1alpha1.KindAgent:      {},
		v1alpha1.KindDeployment: {},
		v1alpha1.KindRuntime:    {},
	}
	server := NewServer(stores, nil, nil)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	require.NoError(t, err)
	defer func() {
		err := serverSession.Wait()
		if err != nil && !errors.Is(err, io.ErrClosedPipe) && !errors.Is(err, io.EOF) {
			require.NoError(t, err)
		}
	}()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	require.NoError(t, err)
	defer func() { _ = clientSession.Close() }()

	result, err := clientSession.ListTools(ctx, nil)
	require.NoError(t, err)
	tools := make(map[string]*mcp.Tool, len(result.Tools))
	for _, tool := range result.Tools {
		tools[tool.Name] = tool
	}

	for _, name := range []string{"list_agents", "get_agent"} {
		raw, err := json.Marshal(tools[name].InputSchema)
		require.NoError(t, err)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		require.Contains(t, schema.Properties, "tag", name)
	}
	for _, name := range []string{"list_deployments", "get_deployment", "list_runtimes", "get_runtime"} {
		raw, err := json.Marshal(tools[name].InputSchema)
		require.NoError(t, err)
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		require.NoError(t, json.Unmarshal(raw, &schema))
		require.NotContains(t, schema.Properties, "tag", name)
	}
}

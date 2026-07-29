package registryserver

import (
	"encoding/json"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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

package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestDeploymentInternalMetaJSON(t *testing.T) {
	meta := DeploymentInternalMeta{
		RuntimeID:         "runtime-1",
		RuntimeName:       "weather",
		RuntimeResourceID: "arn:aws:bedrock-agentcore:us-west-2:123456789012:runtime/runtime-1",
		RuntimeNamespace:  "team-a",
		ActorSubject:      "arn:aws:iam::123456789012:role/weather",
		AppProtocol:       v1alpha1.AgentProtocolA2A,
	}

	encoded, err := json.Marshal(meta)
	require.NoError(t, err)
	require.JSONEq(t, `{
		"runtimeId":"runtime-1",
		"runtimeName":"weather",
		"runtimeResourceId":"arn:aws:bedrock-agentcore:us-west-2:123456789012:runtime/runtime-1",
		"runtimeNamespace":"team-a",
		"actorSubject":"arn:aws:iam::123456789012:role/weather",
		"appProtocol":"A2A"
	}`, string(encoded))
}

func TestRawObjectDoesNotExposeInternalMeta(t *testing.T) {
	object := v1alpha1.RawObject{InternalMeta: json.RawMessage(`{"runtimeId":"runtime-1"}`)}

	encoded, err := json.Marshal(object)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "runtimeId")
	require.NotContains(t, string(encoded), "internalMeta")
}

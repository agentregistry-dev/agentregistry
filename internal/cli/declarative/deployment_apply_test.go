package declarative_test

import (
	"bytes"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	arv0 "github.com/agentregistry-dev/agentregistry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deploymentYAMLBadTemplate is a minimally-valid declarative deployment that
// points at a non-existent agent. Apply rejects this server-side because the
// referenced (name, version) is not a registered agent.
const deploymentYAMLBadTemplate = `apiVersion: ar.dev/v1alpha1
kind: Deployment
metadata:
  name: nonexistent-agent
  version: "0.1.0"
spec:
  targetRef:
    kind: Agent
    name: nonexistent-agent
    version: "0.1.0"
  providerRef:
    kind: Provider
    name: my-provider
`

// TestDeploymentApply_InvalidTemplateRefSurfaces asserts the CLI renders a
// clear error line when the server rejects a deployment whose template does
// not exist. The server reports ApplyStatusFailed + an error message; the
// CLI must pass that through to stdout and exit non-zero.
func TestDeploymentApply_InvalidTemplateRefSurfaces(t *testing.T) {
	results := []arv0.ApplyResult{
		{
			Kind:    "deployment",
			Name:    "nonexistent-agent",
			Version: "0.1.0",
			Status:  arv0.ApplyStatusFailed,
			Error:   `agent "nonexistent-agent" version "0.1.0" not found`,
		},
	}
	srv, _ := newApplyTestServer(t, results)
	setupApplyClient(t, srv)

	var out bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"-f", writeTempYAML(t, deploymentYAMLBadTemplate)})

	err := cmd.Execute()
	require.Error(t, err, "apply must exit non-zero when any result failed")

	output := out.String()
	assert.Contains(t, output, "✗ deployment/nonexistent-agent",
		"failed-status line should identify the offending deployment")
	assert.Contains(t, output, "not found",
		"the server's error message should be surfaced to the user")
}

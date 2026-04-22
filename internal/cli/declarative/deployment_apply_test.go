package declarative_test

import (
	"bytes"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// deploymentYAMLBadTemplate is a minimally-valid declarative deployment that
// points at a non-existent agent. Apply rejects this server-side because the
// referenced (name, version) is not a registered agent.
const deploymentYAMLBadTemplate = `apiVersion: ar.dev/v1alpha1
kind: deployment
metadata:
  name: nonexistent-agent
  version: "0.1.0"
spec:
  resourceType: agent
  providerId: my-provider
`

// TestDeploymentApply_InvalidTemplateRefSurfaces asserts the CLI renders a
// clear error line when the server rejects a deployment whose template does
// not exist. The server reports kinds.StatusFailed + an error message; the
// CLI must pass that through to stdout and exit non-zero.
func TestDeploymentApply_InvalidTemplateRefSurfaces(t *testing.T) {
	results := []kinds.Result{
		{
			Kind:    "deployment",
			Name:    "nonexistent-agent",
			Version: "0.1.0",
			Status:  kinds.StatusFailed,
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

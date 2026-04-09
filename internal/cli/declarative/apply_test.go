package declarative_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyCmd_DryRun(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(yamlPath, []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  image: ghcr.io/acme/bot:latest
  description: "A bot"
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`), 0644)
	require.NoError(t, err)

	declarative.SetAPIClient(nil)

	var buf bytes.Buffer
	cmd := declarative.NewApplyCmd()
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"-f", yamlPath, "--dry-run"})
	err = cmd.Execute()
	require.NoError(t, err)
	output := buf.String()
	assert.Contains(t, output, "[dry-run]")
	assert.Contains(t, output, "agent/acme/bot")
}

func TestApplyCmd_RejectsUnknownKind(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "bad.yaml")
	err := os.WriteFile(yamlPath, []byte(`
apiVersion: ar.dev/v1alpha1
kind: UnknownKind
metadata:
  name: acme/test
  version: "1.0.0"
spec:
  description: "test"
`), 0644)
	require.NoError(t, err)

	declarative.SetAPIClient(nil)
	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", yamlPath, "--dry-run"})
	err = cmd.Execute()
	require.Error(t, err)
	assert.True(t, strings.Contains(err.Error(), "unknown resource type") ||
		strings.Contains(err.Error(), "UnknownKind"))
}

func TestApplyCmd_RejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "invalid.yaml")
	err := os.WriteFile(yamlPath, []byte(`not: valid: yaml: [[[`), 0644)
	require.NoError(t, err)

	declarative.SetAPIClient(nil)
	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", yamlPath, "--dry-run"})
	err = cmd.Execute()
	require.Error(t, err)
}

func TestApplyCmd_NoAPIClientWithoutDryRun(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "test.yaml")
	err := os.WriteFile(yamlPath, []byte(`
apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: acme/bot
  version: "1.0.0"
spec:
  description: "A bot"
  image: ghcr.io/acme/bot:latest
  language: python
  framework: adk
  modelProvider: google
  modelName: gemini-2.0-flash
`), 0644)
	require.NoError(t, err)

	declarative.SetAPIClient(nil)
	cmd := declarative.NewApplyCmd()
	cmd.SetArgs([]string{"-f", yamlPath})
	err = cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "API client not initialized")
}

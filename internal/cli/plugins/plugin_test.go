package plugins

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDescriptor_OK(t *testing.T) {
	yaml := []byte(`
apiVersion: arctl.dev/v1
name: adk-python
type: agent
framework: adk
language: python
description: Google Agent Development Kit
templatesDir: ./templates
env:
  required:
    - OPENAI_API_KEY
  optional:
    - LOG_LEVEL
build:
  command: ["docker", "build", "-t", "{{.Image}}", "."]
run:
  command: ["docker", "compose", "up"]
`)
	got, err := ParseDescriptor(yaml)
	require.NoError(t, err)
	assert.Equal(t, "arctl.dev/v1", got.APIVersion)
	assert.Equal(t, "adk-python", got.Name)
	assert.Equal(t, "agent", got.Type)
	assert.Equal(t, "adk", got.Framework)
	assert.Equal(t, "python", got.Language)
	assert.Equal(t, []string{"OPENAI_API_KEY"}, got.Env.Required)
	assert.Equal(t, []string{"LOG_LEVEL"}, got.Env.Optional)
	assert.Equal(t, []string{"docker", "build", "-t", "{{.Image}}", "."}, got.Build.Command)
	assert.Equal(t, []string{"docker", "compose", "up"}, got.Run.Command)
}

func TestParseDescriptor_RejectsUnsupportedAPIVersion(t *testing.T) {
	yaml := []byte(`apiVersion: arctl.dev/v99
name: foo
type: agent
framework: foo
language: bar
`)
	_, err := ParseDescriptor(yaml)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported apiVersion")
}

func TestParseDescriptor_RejectsMissingRequiredFields(t *testing.T) {
	yaml := []byte(`apiVersion: arctl.dev/v1
name: foo
type: agent
`) // missing framework, language
	_, err := ParseDescriptor(yaml)
	require.Error(t, err)
}

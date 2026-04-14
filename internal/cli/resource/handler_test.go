package resource_test

import (
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/cli/resource"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubHandler is a minimal ResourceHandler for testing the registry.
type stubHandler struct{ kind, singular, plural string }

func (s *stubHandler) Kind() string                                     { return s.kind }
func (s *stubHandler) Singular() string                                 { return s.singular }
func (s *stubHandler) Plural() string                                   { return s.plural }
func (s *stubHandler) Apply(_ *client.Client, _ *scheme.Resource) error { return nil }
func (s *stubHandler) List(_ *client.Client) ([]any, error)             { return nil, nil }
func (s *stubHandler) Get(_ *client.Client, _ string) (any, error)      { return nil, nil }
func (s *stubHandler) Delete(_ *client.Client, _, _ string) error       { return nil }
func (s *stubHandler) TableColumns() []string                           { return nil }
func (s *stubHandler) TableRow(_ any) []string                          { return nil }
func (s *stubHandler) ToResource(_ any) *scheme.Resource                { return nil }

func TestLookup_ByKind(t *testing.T) {
	r := resource.NewRegistry()
	r.Register(&stubHandler{kind: "Widget", singular: "widget", plural: "widgets"})

	h, err := r.Lookup("Widget")
	require.NoError(t, err)
	assert.Equal(t, "Widget", h.Kind())
}

func TestLookup_ByPlural(t *testing.T) {
	r := resource.NewRegistry()
	r.Register(&stubHandler{kind: "Widget", singular: "widget", plural: "widgets"})

	h, err := r.Lookup("widgets")
	require.NoError(t, err)
	assert.Equal(t, "Widget", h.Kind())
}

func TestLookup_BySingular(t *testing.T) {
	r := resource.NewRegistry()
	r.Register(&stubHandler{kind: "Widget", singular: "widget", plural: "widgets"})

	h, err := r.Lookup("widget")
	require.NoError(t, err)
	assert.Equal(t, "Widget", h.Kind())
}

func TestLookup_Unknown(t *testing.T) {
	r := resource.NewRegistry()
	_, err := r.Lookup("Unknown")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown resource type")
}

func TestAgentHandler_Lookup(t *testing.T) {
	h, err := resource.Lookup("agents")
	require.NoError(t, err)
	assert.Equal(t, "Agent", h.Kind())
	assert.Equal(t, "agents", h.Plural())
	assert.Equal(t, "agent", h.Singular())
}

func TestAgentHandler_TableColumns(t *testing.T) {
	h, err := resource.Lookup("Agent")
	require.NoError(t, err)
	cols := h.TableColumns()
	assert.Contains(t, cols, "Name")
	assert.Contains(t, cols, "Version")
	assert.Contains(t, cols, "Framework")
}

func TestAgentHandler_TableRow(t *testing.T) {
	h, err := resource.Lookup("Agent")
	require.NoError(t, err)

	item := &models.AgentResponse{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "acme/bot",
				Framework:     "adk",
				Language:      "python",
				ModelProvider: "google",
				ModelName:     "gemini-2.0-flash",
			},
			Version: "1.0.0",
		},
	}
	row := h.TableRow(item)
	assert.Equal(t, "acme/bot", row[0])
	assert.Equal(t, "1.0.0", row[1])
}

func TestAgentHandler_ToResource(t *testing.T) {
	h, err := resource.Lookup("Agent")
	require.NoError(t, err)

	item := &models.AgentResponse{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "acme/bot",
				Framework:     "adk",
				Language:      "python",
				ModelProvider: "google",
				ModelName:     "gemini-2.0-flash",
				Description:   "A bot",
			},
			Version: "1.0.0",
		},
	}
	r := h.ToResource(item)
	require.NotNil(t, r)
	assert.Equal(t, scheme.APIVersion, r.APIVersion)
	assert.Equal(t, "Agent", r.Kind)
	assert.Equal(t, "acme/bot", r.Metadata.Name)
	assert.Equal(t, "1.0.0", r.Metadata.Version)
	assert.Equal(t, "adk", r.Spec["framework"])
	// updatedAt must not appear in spec
	assert.NotContains(t, r.Spec, "updatedAt")
	// timestamps absent when registry hasn't set them
	assert.Nil(t, r.Metadata.PublishedAt)
	assert.Nil(t, r.Metadata.UpdatedAt)
}

func TestAgentHandler_ToResource_Timestamps(t *testing.T) {
	h, err := resource.Lookup("Agent")
	require.NoError(t, err)

	published := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	item := &models.AgentResponse{
		Agent: models.AgentJSON{
			AgentManifest: models.AgentManifest{
				Name:          "acme/bot",
				Framework:     "adk",
				Language:      "python",
				ModelProvider: "google",
				ModelName:     "gemini-2.0-flash",
				Description:   "A bot",
			},
			Version: "1.0.0",
		},
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				PublishedAt: published,
				UpdatedAt:   updated,
			},
		},
	}
	r := h.ToResource(item)
	require.NotNil(t, r)
	assert.NotContains(t, r.Spec, "updatedAt")
	require.NotNil(t, r.Metadata.PublishedAt)
	assert.Equal(t, published, *r.Metadata.PublishedAt)
	require.NotNil(t, r.Metadata.UpdatedAt)
	assert.Equal(t, updated, *r.Metadata.UpdatedAt)
}

func TestMCPServerHandler_ToResource_Timestamps(t *testing.T) {
	h, err := resource.Lookup("MCPServer")
	require.NoError(t, err)

	published := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	item := &v0.ServerResponse{
		Server: v0.ServerJSON{Name: "acme/fetch", Version: "1.0.0", Description: "Fetches URLs"},
		Meta: v0.ResponseMeta{
			Official: &v0.RegistryExtensions{PublishedAt: published, UpdatedAt: updated},
		},
	}
	r := h.ToResource(item)
	require.NotNil(t, r)
	assert.NotContains(t, r.Spec, "updatedAt")
	require.NotNil(t, r.Metadata.PublishedAt)
	assert.Equal(t, published, *r.Metadata.PublishedAt)
	require.NotNil(t, r.Metadata.UpdatedAt)
	assert.Equal(t, updated, *r.Metadata.UpdatedAt)
}

func TestMCPServerHandler_Lookup(t *testing.T) {
	h, err := resource.Lookup("mcps")
	require.NoError(t, err)
	assert.Equal(t, "MCPServer", h.Kind())
	assert.Equal(t, "mcp", h.Singular())
}

func TestMCPServerHandler_TableRow(t *testing.T) {
	h, err := resource.Lookup("MCPServer")
	require.NoError(t, err)

	item := &v0.ServerResponse{
		Server: v0.ServerJSON{
			Name:        "acme/fetch",
			Version:     "1.0.0",
			Description: "Fetches URLs",
		},
	}
	row := h.TableRow(item)
	assert.Equal(t, "acme/fetch", row[0])
	assert.Equal(t, "1.0.0", row[1])
}

func TestSkillHandler_Lookup(t *testing.T) {
	h, err := resource.Lookup("skills")
	require.NoError(t, err)
	assert.Equal(t, "Skill", h.Kind())
}

func TestSkillHandler_TableRow(t *testing.T) {
	h, err := resource.Lookup("Skill")
	require.NoError(t, err)

	item := &models.SkillResponse{
		Skill: models.SkillJSON{
			Name:        "acme/summarize",
			Version:     "1.0.0",
			Description: "Summarizes text",
		},
	}
	row := h.TableRow(item)
	assert.Equal(t, "acme/summarize", row[0])
	assert.Equal(t, "1.0.0", row[1])
}

func TestSkillHandler_ToResource_Timestamps(t *testing.T) {
	h, err := resource.Lookup("Skill")
	require.NoError(t, err)

	published := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	item := &models.SkillResponse{
		Skill: models.SkillJSON{Name: "acme/summarize", Version: "1.0.0", Description: "Summarizes text"},
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{PublishedAt: published, UpdatedAt: updated},
		},
	}
	r := h.ToResource(item)
	require.NotNil(t, r)
	assert.NotContains(t, r.Spec, "updatedAt")
	require.NotNil(t, r.Metadata.PublishedAt)
	assert.Equal(t, published, *r.Metadata.PublishedAt)
	require.NotNil(t, r.Metadata.UpdatedAt)
	assert.Equal(t, updated, *r.Metadata.UpdatedAt)
}

func TestPromptHandler_Lookup(t *testing.T) {
	h, err := resource.Lookup("prompts")
	require.NoError(t, err)
	assert.Equal(t, "Prompt", h.Kind())
}

func TestPromptHandler_ToResource_Timestamps(t *testing.T) {
	h, err := resource.Lookup("Prompt")
	require.NoError(t, err)

	published := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)

	item := &models.PromptResponse{
		Prompt: models.PromptJSON{Name: "acme/system", Version: "1.0.0", Content: "You are helpful."},
		Meta: models.PromptResponseMeta{
			Official: &models.PromptRegistryExtensions{PublishedAt: published, UpdatedAt: updated},
		},
	}
	r := h.ToResource(item)
	require.NotNil(t, r)
	assert.NotContains(t, r.Spec, "updatedAt")
	require.NotNil(t, r.Metadata.PublishedAt)
	assert.Equal(t, published, *r.Metadata.PublishedAt)
	require.NotNil(t, r.Metadata.UpdatedAt)
	assert.Equal(t, updated, *r.Metadata.UpdatedAt)
}

func TestPromptHandler_TableRow(t *testing.T) {
	h, err := resource.Lookup("Prompt")
	require.NoError(t, err)

	item := &models.PromptResponse{
		Prompt: models.PromptJSON{
			Name:    "acme/system",
			Version: "1.0.0",
			Content: "You are a helpful assistant.",
		},
	}
	row := h.TableRow(item)
	assert.Equal(t, "acme/system", row[0])
	assert.Equal(t, "1.0.0", row[1])
}

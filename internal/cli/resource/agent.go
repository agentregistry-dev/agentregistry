package resource

import (
	"encoding/json"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
)

func init() {
	Register(&AgentHandler{})
}

// AgentHandler implements ResourceHandler for the Agent kind.
type AgentHandler struct{}

func (h *AgentHandler) Kind() string     { return "Agent" }
func (h *AgentHandler) Singular() string { return "agent" }
func (h *AgentHandler) Plural() string   { return "agents" }

func (h *AgentHandler) Apply(c *client.Client, r *scheme.Resource) error {
	agentJSON, err := h.toAgentJSON(r)
	if err != nil {
		return err
	}
	_, err = c.ApplyAgent(agentJSON.Name, agentJSON.Version, agentJSON)
	return err
}

func (h *AgentHandler) List(c *client.Client) ([]any, error) {
	agents, err := c.GetAgents()
	if err != nil {
		return nil, err
	}
	items := make([]any, len(agents))
	for i, a := range agents {
		items[i] = a
	}
	return items, nil
}

func (h *AgentHandler) Get(c *client.Client, name string) (any, error) {
	return c.GetAgent(name)
}

func (h *AgentHandler) Delete(c *client.Client, name, version string) error {
	return c.DeleteAgent(name, version)
}

func (h *AgentHandler) TableColumns() []string {
	return []string{"Name", "Version", "Framework", "Language", "Provider", "Model"}
}

func (h *AgentHandler) TableRow(item any) []string {
	a, ok := item.(*models.AgentResponse)
	if !ok {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(a.Agent.Name, 40),
		a.Agent.Version,
		printer.EmptyValueOrDefault(a.Agent.Framework, "<none>"),
		printer.EmptyValueOrDefault(a.Agent.Language, "<none>"),
		printer.EmptyValueOrDefault(a.Agent.ModelProvider, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(a.Agent.ModelName, "<none>"), 30),
	}
}

func (h *AgentHandler) ToResource(item any) *scheme.Resource {
	a, ok := item.(*models.AgentResponse)
	if !ok {
		return nil
	}
	b, _ := json.Marshal(a.Agent)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	delete(spec, "name")
	delete(spec, "version")
	delete(spec, "updatedAt")
	delete(spec, "status")
	delete(spec, "publishedAt")

	meta := scheme.Metadata{
		Name:    a.Agent.Name,
		Version: a.Agent.Version,
	}
	if a.Meta.Official != nil {
		if !a.Meta.Official.PublishedAt.IsZero() {
			t := a.Meta.Official.PublishedAt
			meta.PublishedAt = &t
		}
		if !a.Meta.Official.UpdatedAt.IsZero() {
			t := a.Meta.Official.UpdatedAt
			meta.UpdatedAt = &t
		}
	}

	return &scheme.Resource{
		APIVersion: scheme.APIVersion,
		Kind:       "Agent",
		Metadata:   meta,
		Spec:       spec,
	}
}

func (h *AgentHandler) toAgentJSON(r *scheme.Resource) (*models.AgentJSON, error) {
	b, err := json.Marshal(r.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling Agent spec: %w", err)
	}
	var agentJSON models.AgentJSON
	if err := json.Unmarshal(b, &agentJSON); err != nil {
		return nil, fmt.Errorf("invalid Agent spec: %w", err)
	}
	agentJSON.Name = r.Metadata.Name
	agentJSON.Version = r.Metadata.Version
	if agentJSON.Status == "" {
		agentJSON.Status = "active"
	}
	return &agentJSON, nil
}

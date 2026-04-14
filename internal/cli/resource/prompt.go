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
	Register(&PromptHandler{})
}

type PromptHandler struct{}

func (h *PromptHandler) Kind() string     { return "Prompt" }
func (h *PromptHandler) Singular() string { return "prompt" }
func (h *PromptHandler) Plural() string   { return "prompts" }

func (h *PromptHandler) Apply(c *client.Client, r *scheme.Resource) error {
	promptJSON, err := h.toPromptJSON(r)
	if err != nil {
		return err
	}
	_, err = c.ApplyPrompt(promptJSON.Name, promptJSON.Version, promptJSON)
	return err
}

func (h *PromptHandler) List(c *client.Client) ([]any, error) {
	prompts, err := c.GetPrompts()
	if err != nil {
		return nil, err
	}
	items := make([]any, len(prompts))
	for i, p := range prompts {
		items[i] = p
	}
	return items, nil
}

func (h *PromptHandler) Get(c *client.Client, name string) (any, error) {
	return c.GetPrompt(name)
}

func (h *PromptHandler) Delete(c *client.Client, name, version string) error {
	return c.DeletePrompt(name, version)
}

func (h *PromptHandler) TableColumns() []string {
	return []string{"Name", "Version", "Description"}
}

func (h *PromptHandler) TableRow(item any) []string {
	p, ok := item.(*models.PromptResponse)
	if !ok {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(p.Prompt.Name, 40),
		p.Prompt.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(p.Prompt.Description, "<none>"), 60),
	}
}

func (h *PromptHandler) ToResource(item any) *scheme.Resource {
	p, ok := item.(*models.PromptResponse)
	if !ok {
		return nil
	}
	b, _ := json.Marshal(p.Prompt)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	delete(spec, "name")
	delete(spec, "version")
	delete(spec, "updatedAt")
	delete(spec, "status")
	delete(spec, "publishedAt")

	meta := scheme.Metadata{Name: p.Prompt.Name, Version: p.Prompt.Version}
	if p.Meta.Official != nil {
		if !p.Meta.Official.PublishedAt.IsZero() {
			t := p.Meta.Official.PublishedAt
			meta.PublishedAt = &t
		}
		if !p.Meta.Official.UpdatedAt.IsZero() {
			t := p.Meta.Official.UpdatedAt
			meta.UpdatedAt = &t
		}
	}
	return &scheme.Resource{
		APIVersion: scheme.APIVersion,
		Kind:       "Prompt",
		Metadata:   meta,
		Spec:       spec,
	}
}

func (h *PromptHandler) toPromptJSON(r *scheme.Resource) (*models.PromptJSON, error) {
	b, err := json.Marshal(r.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling Prompt spec: %w", err)
	}
	var promptJSON models.PromptJSON
	if err := json.Unmarshal(b, &promptJSON); err != nil {
		return nil, fmt.Errorf("invalid Prompt spec: %w", err)
	}
	promptJSON.Name = r.Metadata.Name
	promptJSON.Version = r.Metadata.Version
	return &promptJSON, nil
}

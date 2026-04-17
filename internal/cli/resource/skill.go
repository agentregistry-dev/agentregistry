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
	Register(&SkillHandler{})
}

type SkillHandler struct{}

func (h *SkillHandler) Kind() string     { return "Skill" }
func (h *SkillHandler) Singular() string { return "skill" }
func (h *SkillHandler) Plural() string   { return "skills" }

func (h *SkillHandler) Apply(c *client.Client, r *scheme.Resource) error {
	skillJSON, err := h.toSkillJSON(r)
	if err != nil {
		return err
	}
	_, err = c.ApplySkill(skillJSON.Name, skillJSON.Version, skillJSON)
	return err
}

func (h *SkillHandler) List(c *client.Client) ([]any, error) {
	skills, err := c.GetSkills()
	if err != nil {
		return nil, err
	}
	items := make([]any, len(skills))
	for i, s := range skills {
		items[i] = s
	}
	return items, nil
}

func (h *SkillHandler) Get(c *client.Client, name string) (any, error) {
	return c.GetSkill(name)
}

func (h *SkillHandler) Delete(c *client.Client, name, version string) error {
	return c.DeleteSkill(name, version)
}

func (h *SkillHandler) TableColumns() []string {
	return []string{"Name", "Version", "Category", "Description"}
}

func (h *SkillHandler) TableRow(item any) []string {
	s, ok := item.(*models.SkillResponse)
	if !ok {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(s.Skill.Name, 40),
		s.Skill.Version,
		printer.EmptyValueOrDefault(s.Skill.Category, "<none>"),
		printer.TruncateString(printer.EmptyValueOrDefault(s.Skill.Description, "<none>"), 60),
	}
}

func (h *SkillHandler) ToResource(item any) *scheme.Resource {
	s, ok := item.(*models.SkillResponse)
	if !ok {
		return nil
	}
	b, _ := json.Marshal(s.Skill)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	delete(spec, "name")
	delete(spec, "version")
	delete(spec, "updatedAt")
	delete(spec, "status")
	delete(spec, "publishedAt")

	meta := scheme.Metadata{Name: s.Skill.Name, Version: s.Skill.Version}
	if s.Meta.Official != nil {
		if !s.Meta.Official.PublishedAt.IsZero() {
			t := s.Meta.Official.PublishedAt
			meta.PublishedAt = &t
		}
		if !s.Meta.Official.UpdatedAt.IsZero() {
			t := s.Meta.Official.UpdatedAt
			meta.UpdatedAt = &t
		}
	}
	return &scheme.Resource{
		APIVersion: scheme.APIVersion,
		Kind:       "Skill",
		Metadata:   meta,
		Spec:       spec,
	}
}

func (h *SkillHandler) toSkillJSON(r *scheme.Resource) (*models.SkillJSON, error) {
	b, err := json.Marshal(r.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling Skill spec: %w", err)
	}
	var skillJSON models.SkillJSON
	if err := json.Unmarshal(b, &skillJSON); err != nil {
		return nil, fmt.Errorf("invalid Skill spec: %w", err)
	}
	skillJSON.Name = r.Metadata.Name
	skillJSON.Version = r.Metadata.Version
	return &skillJSON, nil
}

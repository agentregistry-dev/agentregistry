package resource

import (
	"encoding/json"
	"fmt"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

func init() {
	Register(&MCPServerHandler{})
}

// MCPServerHandler implements ResourceHandler for the MCPServer kind.
type MCPServerHandler struct{}

func (h *MCPServerHandler) Kind() string     { return "MCPServer" }
func (h *MCPServerHandler) Singular() string { return "mcp" }
func (h *MCPServerHandler) Plural() string   { return "mcps" }

func (h *MCPServerHandler) Apply(c *client.Client, r *scheme.Resource) error {
	serverJSON, err := h.toServerJSON(r)
	if err != nil {
		return err
	}
	_, err = c.ApplyMCPServer(serverJSON.Name, serverJSON.Version, serverJSON)
	return err
}

func (h *MCPServerHandler) List(c *client.Client) ([]any, error) {
	servers, err := c.GetPublishedServers()
	if err != nil {
		return nil, err
	}
	items := make([]any, len(servers))
	for i, s := range servers {
		items[i] = s
	}
	return items, nil
}

func (h *MCPServerHandler) Get(c *client.Client, name string) (any, error) {
	return c.GetServer(name)
}

func (h *MCPServerHandler) Delete(c *client.Client, name, version string) error {
	return c.DeleteMCPServer(name, version)
}

func (h *MCPServerHandler) TableColumns() []string {
	return []string{"Name", "Version", "Description"}
}

func (h *MCPServerHandler) TableRow(item any) []string {
	s, ok := item.(*v0.ServerResponse)
	if !ok {
		return []string{"<invalid>"}
	}
	return []string{
		printer.TruncateString(s.Server.Name, 40),
		s.Server.Version,
		printer.TruncateString(printer.EmptyValueOrDefault(s.Server.Description, "<none>"), 60),
	}
}

func (h *MCPServerHandler) ToResource(item any) *scheme.Resource {
	s, ok := item.(*v0.ServerResponse)
	if !ok {
		return nil
	}
	b, _ := json.Marshal(s.Server)
	var spec map[string]any
	_ = json.Unmarshal(b, &spec)
	delete(spec, "name")
	delete(spec, "version")
	delete(spec, "updatedAt")
	delete(spec, "status")
	delete(spec, "publishedAt")

	meta := scheme.Metadata{
		Name:    s.Server.Name,
		Version: s.Server.Version,
	}
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
		Kind:       "MCPServer",
		Metadata:   meta,
		Spec:       spec,
	}
}

func (h *MCPServerHandler) toServerJSON(r *scheme.Resource) (*v0.ServerJSON, error) {
	b, err := json.Marshal(r.Spec)
	if err != nil {
		return nil, fmt.Errorf("marshalling MCPServer spec: %w", err)
	}
	var serverJSON v0.ServerJSON
	if err := json.Unmarshal(b, &serverJSON); err != nil {
		return nil, fmt.Errorf("invalid MCPServer spec: %w", err)
	}
	serverJSON.Name = r.Metadata.Name
	serverJSON.Version = r.Metadata.Version
	if serverJSON.Schema == "" {
		serverJSON.Schema = model.CurrentSchemaURL
	}
	return &serverJSON, nil
}

package declarative

import (
	"context"
	"reflect"

	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/printer"
	v0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
)

var apiClient *client.Client

// SetAPIClient sets the API client used by all declarative commands.
// Called by pkg/cli/root.go's PersistentPreRunE.
func SetAPIClient(c *client.Client) {
	apiClient = c
}

// defaultRegistry is the kinds.Registry used by the declarative CLI for YAML decoding.
// It is populated at package init time with decode-only (no service) kind entries
// so that arctl can parse YAML files without a live registry connection.
var defaultRegistry = newCLIRegistry()

// SetRegistry replaces the default decoding registry. Useful for tests and for
// enterprise extensions that register additional kinds.
func SetRegistry(r *kinds.Registry) {
	defaultRegistry = r
}

// NewCLIRegistry builds a decode-only registry containing the four built-in
// kinds. Service functions (Apply, Get, Delete) are intentionally omitted here;
// they are wired by the server-side kind packages (internal/registry/kinds/*).
// Exported for use in tests that need to restore the default registry.
func NewCLIRegistry() *kinds.Registry {
	return newCLIRegistry()
}

func newCLIRegistry() *kinds.Registry {
	reg := kinds.NewRegistry()
	reg.Register(kinds.Kind{
		Kind:     "agent",
		Plural:   "agents",
		Aliases:  []string{"Agent"},
		SpecType: reflect.TypeFor[kinds.AgentSpec](),
		Get: func(_ context.Context, name, _ string) (any, error) {
			return apiClient.GetAgent(name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return apiClient.DeleteAgent(name, version)
		},
		ListFunc: kinds.MakeListFunc(func() ([]*models.AgentResponse, error) {
			return apiClient.GetAgents()
		}),
		RowFunc: func(item any) []string {
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
		},
		ToResourceFunc: func(item any) *kinds.Document {
			a, ok := item.(*models.AgentResponse)
			if !ok {
				return nil
			}
			return &kinds.Document{
				APIVersion: scheme.APIVersion,
				Kind:       "Agent",
				Metadata:   kinds.Metadata{Name: a.Agent.Name, Version: a.Agent.Version},
				Spec:       marshalToSpec(a.Agent),
			}
		},
		TableColumns: []kinds.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "FRAMEWORK"},
			{Header: "LANGUAGE"},
			{Header: "PROVIDER"},
			{Header: "MODEL"},
		},
	})
	reg.Register(kinds.Kind{
		Kind:     "mcp",
		Plural:   "mcps",
		Aliases:  []string{"MCPServer", "mcpserver", "mcp-server", "mcpservers"},
		SpecType: reflect.TypeFor[kinds.MCPSpec](),
		Get: func(_ context.Context, name, _ string) (any, error) {
			return apiClient.GetServer(name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return apiClient.DeleteMCPServer(name, version)
		},
		ListFunc: kinds.MakeListFunc(func() ([]*v0.ServerResponse, error) {
			return apiClient.GetPublishedServers()
		}),
		RowFunc: func(item any) []string {
			s, ok := item.(*v0.ServerResponse)
			if !ok {
				return []string{"<invalid>"}
			}
			return []string{
				printer.TruncateString(s.Server.Name, 40),
				s.Server.Version,
				printer.TruncateString(printer.EmptyValueOrDefault(s.Server.Description, "<none>"), 60),
			}
		},
		ToResourceFunc: func(item any) *kinds.Document {
			s, ok := item.(*v0.ServerResponse)
			if !ok {
				return nil
			}
			return &kinds.Document{
				APIVersion: scheme.APIVersion,
				Kind:       "MCPServer",
				Metadata:   kinds.Metadata{Name: s.Server.Name, Version: s.Server.Version},
				Spec:       marshalToSpec(s.Server),
			}
		},
		TableColumns: []kinds.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "DESCRIPTION"},
		},
	})
	reg.Register(kinds.Kind{
		Kind:     "skill",
		Plural:   "skills",
		Aliases:  []string{"Skill"},
		SpecType: reflect.TypeFor[kinds.SkillSpec](),
		Get: func(_ context.Context, name, _ string) (any, error) {
			return apiClient.GetSkill(name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return apiClient.DeleteSkill(name, version)
		},
		ListFunc: kinds.MakeListFunc(func() ([]*models.SkillResponse, error) {
			return apiClient.GetSkills()
		}),
		RowFunc: func(item any) []string {
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
		},
		ToResourceFunc: func(item any) *kinds.Document {
			s, ok := item.(*models.SkillResponse)
			if !ok {
				return nil
			}
			return &kinds.Document{
				APIVersion: scheme.APIVersion,
				Kind:       "Skill",
				Metadata:   kinds.Metadata{Name: s.Skill.Name, Version: s.Skill.Version},
				Spec:       marshalToSpec(s.Skill),
			}
		},
		TableColumns: []kinds.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "CATEGORY"},
			{Header: "DESCRIPTION"},
		},
	})
	reg.Register(kinds.Kind{
		Kind:     "prompt",
		Plural:   "prompts",
		Aliases:  []string{"Prompt"},
		SpecType: reflect.TypeFor[kinds.PromptSpec](),
		Get: func(_ context.Context, name, _ string) (any, error) {
			return apiClient.GetPrompt(name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return apiClient.DeletePrompt(name, version)
		},
		ListFunc: kinds.MakeListFunc(func() ([]*models.PromptResponse, error) {
			return apiClient.GetPrompts()
		}),
		RowFunc: func(item any) []string {
			p, ok := item.(*models.PromptResponse)
			if !ok {
				return []string{"<invalid>"}
			}
			return []string{
				printer.TruncateString(p.Prompt.Name, 40),
				p.Prompt.Version,
				printer.TruncateString(printer.EmptyValueOrDefault(p.Prompt.Description, "<none>"), 60),
			}
		},
		ToResourceFunc: func(item any) *kinds.Document {
			p, ok := item.(*models.PromptResponse)
			if !ok {
				return nil
			}
			return &kinds.Document{
				APIVersion: scheme.APIVersion,
				Kind:       "Prompt",
				Metadata:   kinds.Metadata{Name: p.Prompt.Name, Version: p.Prompt.Version},
				Spec:       marshalToSpec(p.Prompt),
			}
		},
		TableColumns: []kinds.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "DESCRIPTION"},
		},
	})
	reg.Register(kinds.Kind{
		Kind:     "provider",
		Plural:   "providers",
		Aliases:  []string{"Provider"},
		SpecType: reflect.TypeFor[kinds.ProviderSpec](),
		TableColumns: []kinds.Column{
			{Header: "NAME"}, {Header: "PLATFORM"},
		},
		ListFunc: kinds.MakeListFunc(func() ([]*models.Provider, error) {
			return apiClient.GetProviders()
		}),
		RowFunc: func(item any) []string {
			p := item.(*models.Provider)
			return []string{p.Name, p.Platform}
		},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return apiClient.GetProvider(name)
		},
		Delete: func(_ context.Context, name, _ string) error {
			return apiClient.DeleteProvider(name)
		},
	})
	reg.Register(kinds.Kind{
		Kind:     "deployment",
		Plural:   "deployments",
		Aliases:  []string{"Deployment"},
		SpecType: reflect.TypeFor[kinds.DeploymentSpec](),
		TableColumns: []kinds.Column{
			{Header: "ID"}, {Header: "NAME"}, {Header: "VERSION"},
			{Header: "TYPE"}, {Header: "PROVIDER"}, {Header: "STATUS"},
		},
		ListFunc: kinds.MakeListFunc(func() ([]*models.Deployment, error) {
			resp, err := apiClient.GetDeployedServers()
			if err != nil {
				return nil, err
			}
			result := make([]*models.Deployment, len(resp))
			copy(result, resp)
			return result, nil
		}),
		RowFunc: func(item any) []string {
			d := item.(*models.Deployment)
			return []string{d.ID, d.ServerName, d.Version, d.ResourceType, d.ProviderID, d.Status}
		},
	})
	return reg
}

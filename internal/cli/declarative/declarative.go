package declarative

import (
	"context"
	"time"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

var apiClient *client.Client

// SetAPIClient sets the API client used by all declarative commands.
// Called by pkg/cli/root.go's PersistentPreRunE.
func SetAPIClient(c *client.Client) {
	apiClient = c
}

// defaultRegistry is the declarative CLI's kind registry. It owns user-facing
// aliases, table metadata, and list/get/delete dispatch, while YAML decode itself
// flows through pkg/api/v1alpha1.Scheme.
var defaultRegistry = newCLIRegistry()

// SetRegistry replaces the default registry. Useful for tests and enterprise extensions.
func SetRegistry(r *scheme.Registry) {
	defaultRegistry = r
}

// NewCLIRegistry returns the built-in declarative registry.
func NewCLIRegistry() *scheme.Registry {
	return newCLIRegistry()
}

func newCLIRegistry() *scheme.Registry {
	reg := scheme.NewRegistry()

	reg.Register(scheme.Kind{
		Kind:    "agent",
		Plural:  "agents",
		Aliases: []string{"Agent"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindAgent, name, "", func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindAgent, name, version, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindAgent, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		RowFunc: func(item any) []string {
			agent, ok := item.(*v1alpha1.Agent)
			if !ok {
				return []string{"<invalid>"}
			}
			return agentRow(agent)
		},
		ToYAMLFunc: func(item any) any { return item },
		TableColumns: []scheme.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "FRAMEWORK"},
			{Header: "LANGUAGE"},
			{Header: "PROVIDER"},
			{Header: "MODEL"},
		},
	})

	reg.Register(scheme.Kind{
		Kind:    "mcp",
		Plural:  "mcps",
		Aliases: []string{"MCPServer", "mcpserver", "mcp-server", "mcpservers"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindMCPServer, name, "", func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindMCPServer, name, version, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindMCPServer, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		RowFunc: func(item any) []string {
			server, ok := item.(*v1alpha1.MCPServer)
			if !ok {
				return []string{"<invalid>"}
			}
			return mcpRow(server)
		},
		ToYAMLFunc: func(item any) any { return item },
		TableColumns: []scheme.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "DESCRIPTION"},
		},
	})

	reg.Register(scheme.Kind{
		Kind:    "skill",
		Plural:  "skills",
		Aliases: []string{"Skill"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindSkill, name, "", func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindSkill, name, version, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindSkill, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		RowFunc: func(item any) []string {
			skill, ok := item.(*v1alpha1.Skill)
			if !ok {
				return []string{"<invalid>"}
			}
			return skillRow(skill)
		},
		ToYAMLFunc: func(item any) any { return item },
		TableColumns: []scheme.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "CATEGORY"},
			{Header: "DESCRIPTION"},
		},
	})

	reg.Register(scheme.Kind{
		Kind:    "prompt",
		Plural:  "prompts",
		Aliases: []string{"Prompt"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindPrompt, name, "", func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindPrompt, name, version, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindPrompt, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		RowFunc: func(item any) []string {
			prompt, ok := item.(*v1alpha1.Prompt)
			if !ok {
				return []string{"<invalid>"}
			}
			return promptRow(prompt)
		},
		ToYAMLFunc: func(item any) any { return item },
		TableColumns: []scheme.Column{
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "DESCRIPTION"},
		},
	})

	reg.Register(scheme.Kind{
		Kind:    "provider",
		Plural:  "providers",
		Aliases: []string{"Provider"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindProvider, name, "", func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		},
		Delete: func(_ context.Context, name, _ string) error {
			return deleteAny(context.Background(), v1alpha1.KindProvider, name, "", func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindProvider, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		},
		RowFunc: func(item any) []string {
			provider, ok := item.(*v1alpha1.Provider)
			if !ok {
				return []string{"<invalid>"}
			}
			return providerRow(provider)
		},
		ToYAMLFunc: func(item any) any { return item },
		TableColumns: []scheme.Column{
			{Header: "NAME"},
			{Header: "PLATFORM"},
		},
	})

	reg.Register(scheme.Kind{
		Kind:    "deployment",
		Plural:  "deployments",
		Aliases: []string{"Deployment"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getDeploymentByTarget(context.Background(), name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteDeploymentByTarget(context.Background(), name, version)
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listDeploymentAny(context.Background())
		},
		RowFunc: func(item any) []string {
			deployment, ok := item.(*cliCommon.DeploymentRecord)
			if !ok {
				return []string{"<invalid>"}
			}
			return deploymentRow(deployment)
		},
		ToYAMLFunc: func(item any) any {
			deployment, ok := item.(*cliCommon.DeploymentRecord)
			if !ok {
				return nil
			}
			return deploymentToDocument(deployment)
		},
		TableColumns: []scheme.Column{
			{Header: "ID"},
			{Header: "NAME"},
			{Header: "VERSION"},
			{Header: "TYPE"},
			{Header: "PROVIDER"},
			{Header: "STATUS"},
		},
	})

	return reg
}

// deploymentStatus is the shape emitted under .status when a deployment is
// rendered as YAML/JSON. This is intentionally a CLI projection rather than the
// raw v1alpha1.Status conditions block so imperative users keep the compact
// fields they already consume while apply decode still ignores incoming status.
type deploymentStatus struct {
	ID               string         `json:"id,omitempty" yaml:"id,omitempty"`
	Phase            string         `json:"phase,omitempty" yaml:"phase,omitempty"`
	Origin           string         `json:"origin,omitempty" yaml:"origin,omitempty"`
	Error            string         `json:"error,omitempty" yaml:"error,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty" yaml:"providerMetadata,omitempty"`
	DeployedAt       time.Time      `json:"deployedAt,omitempty" yaml:"deployedAt,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
}

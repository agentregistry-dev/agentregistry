package declarative

import (
	"context"
	"reflect"
	"time"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/internal/registry/kinds"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
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
			return getAny(context.Background(), v1alpha1.KindAgent, name, "", func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindAgent, name, version, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindAgent, func() *v1alpha1.Agent { return &v1alpha1.Agent{} })
		},
		RowFunc: func(item any) []string {
			a, ok := item.(*v1alpha1.Agent)
			if !ok {
				return []string{"<invalid>"}
			}
			return agentRow(a)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			a, ok := item.(*v1alpha1.Agent)
			if !ok {
				return nil
			}
			return agentToDocument(a)
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
			return getAny(context.Background(), v1alpha1.KindMCPServer, name, "", func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindMCPServer, name, version, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindMCPServer, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
		},
		RowFunc: func(item any) []string {
			s, ok := item.(*v1alpha1.MCPServer)
			if !ok {
				return []string{"<invalid>"}
			}
			return mcpRow(s)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			s, ok := item.(*v1alpha1.MCPServer)
			if !ok {
				return nil
			}
			return mcpToDocument(s)
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
			return getAny(context.Background(), v1alpha1.KindSkill, name, "", func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindSkill, name, version, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindSkill, func() *v1alpha1.Skill { return &v1alpha1.Skill{} })
		},
		RowFunc: func(item any) []string {
			s, ok := item.(*v1alpha1.Skill)
			if !ok {
				return []string{"<invalid>"}
			}
			return skillRow(s)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			s, ok := item.(*v1alpha1.Skill)
			if !ok {
				return nil
			}
			return skillToDocument(s)
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
			return getAny(context.Background(), v1alpha1.KindPrompt, name, "", func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteAny(context.Background(), v1alpha1.KindPrompt, name, version, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindPrompt, func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} })
		},
		RowFunc: func(item any) []string {
			p, ok := item.(*v1alpha1.Prompt)
			if !ok {
				return []string{"<invalid>"}
			}
			return promptRow(p)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			p, ok := item.(*v1alpha1.Prompt)
			if !ok {
				return nil
			}
			return promptToDocument(p)
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
		ListFunc: func(_ context.Context) ([]any, error) {
			return listLatestAny(context.Background(), v1alpha1.KindProvider, func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		},
		RowFunc: func(item any) []string {
			p := item.(*v1alpha1.Provider)
			return providerRow(p)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			p, ok := item.(*v1alpha1.Provider)
			if !ok {
				return nil
			}
			return providerToDocument(p)
		},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getAny(context.Background(), v1alpha1.KindProvider, name, "", func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
		},
		Delete: func(_ context.Context, name, _ string) error {
			return deleteAny(context.Background(), v1alpha1.KindProvider, name, "", func() *v1alpha1.Provider { return &v1alpha1.Provider{} })
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
		ListFunc: func(_ context.Context) ([]any, error) { return listDeploymentAny(context.Background()) },
		RowFunc: func(item any) []string {
			d := item.(*cliCommon.DeploymentRecord)
			return deploymentRow(d)
		},
		ToResourceFunc: func(item any) *kinds.Document {
			d, ok := item.(*cliCommon.DeploymentRecord)
			if !ok {
				return nil
			}
			return deploymentToDocument(d)
		},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getDeploymentByTarget(context.Background(), name)
		},
		Delete: func(_ context.Context, name, version string) error {
			return deleteDeploymentByTarget(context.Background(), name, version)
		},
	})
	return reg
}

// deploymentStatus is the shape emitted under .status when a deployment is
// rendered as YAML/JSON. Server-managed fields only — the envelope decoder
// ignores .status on apply, so this is safe to include in get output without
// breaking round-trips through `arctl apply -f`.
type deploymentStatus struct {
	ID               string         `json:"id,omitempty" yaml:"id,omitempty"`
	Phase            string         `json:"phase,omitempty" yaml:"phase,omitempty"`
	Origin           string         `json:"origin,omitempty" yaml:"origin,omitempty"`
	Error            string         `json:"error,omitempty" yaml:"error,omitempty"`
	ProviderMetadata map[string]any `json:"providerMetadata,omitempty" yaml:"providerMetadata,omitempty"`
	DeployedAt       time.Time      `json:"deployedAt,omitempty" yaml:"deployedAt,omitempty"`
	UpdatedAt        time.Time      `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
}

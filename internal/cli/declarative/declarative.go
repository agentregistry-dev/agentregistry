package declarative

import (
	"context"

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

func init() {
	scheme.Register(typedKind(
		"agent", "agents", []string{"Agent"},
		[]scheme.Column{
			{Header: "NAME"}, {Header: "VERSION"}, {Header: "FRAMEWORK"},
			{Header: "LANGUAGE"}, {Header: "PROVIDER"}, {Header: "MODEL"},
		},
		v1alpha1.KindAgent,
		func() *v1alpha1.Agent { return &v1alpha1.Agent{} },
		agentRow,
	))

	scheme.Register(typedKind(
		"mcp", "mcps", []string{"MCPServer", "mcpserver", "mcp-server", "mcpservers"},
		[]scheme.Column{{Header: "NAME"}, {Header: "VERSION"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindMCPServer,
		func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		mcpRow,
	))

	scheme.Register(typedKind(
		"skill", "skills", []string{"Skill"},
		[]scheme.Column{
			{Header: "NAME"}, {Header: "VERSION"},
			{Header: "CATEGORY"}, {Header: "DESCRIPTION"},
		},
		v1alpha1.KindSkill,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
		skillRow,
	))

	scheme.Register(typedKind(
		"prompt", "prompts", []string{"Prompt"},
		[]scheme.Column{{Header: "NAME"}, {Header: "VERSION"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindPrompt,
		func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} },
		promptRow,
	))

	scheme.Register(typedKind(
		"provider", "providers", []string{"Provider"},
		[]scheme.Column{{Header: "NAME"}, {Header: "PLATFORM"}},
		v1alpha1.KindProvider,
		func() *v1alpha1.Provider { return &v1alpha1.Provider{} },
		providerRow,
	))

	// Deployment is registered manually because its Get/Delete dispatch
	// does NOT key on the v1alpha1 metadata identity (namespace/name/
	// version). Users address deployments by the underlying target's name
	// — `arctl get deployment <agent-or-mcp-name>` — and the CLI walks the
	// /v0/deployments listing to find the matching row. The typed
	// helper assumes (kind, namespace, name, version) lookup, which is
	// the wrong shape for this dispatch.
	scheme.Register(&scheme.Kind{
		Kind:    "deployment",
		Plural:  "deployments",
		Aliases: []string{"Deployment"},
		Get: func(_ context.Context, name, _ string) (any, error) {
			return getDeploymentByTarget(context.Background(), name)
		},
		Delete: func(_ context.Context, name, version string, force bool) error {
			return deleteDeploymentByTarget(context.Background(), name, version, force)
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
			{Header: "ID"}, {Header: "NAME"}, {Header: "VERSION"},
			{Header: "TYPE"}, {Header: "PROVIDER"}, {Header: "STATUS"},
		},
	})
}

// typedKind builds a scheme.Kind whose Get / List / Delete dispatch
// closures all wire through the typed v1alpha1 client helpers
// (client.GetTyped[T] / client.ListAllTyped[T] / apiClient.Delete) for
// the canonical kind. Per-kind callers supply the user-facing name +
// aliases, the table layout, and a row formatter that takes the typed
// envelope T directly. RowFunc shape-checks the input via T-assertion
// so the registry's `any` API stays internal.
func typedKind[T v1alpha1.Object](
	cliName, plural string,
	aliases []string,
	columns []scheme.Column,
	canonicalKind string,
	newObj func() T,
	row func(T) []string,
) *scheme.Kind {
	return &scheme.Kind{
		Kind:         cliName,
		Plural:       plural,
		Aliases:      aliases,
		TableColumns: columns,
		ToYAMLFunc:   func(item any) any { return item },
		RowFunc: func(item any) []string {
			t, ok := item.(T)
			if !ok {
				return []string{"<invalid>"}
			}
			return row(t)
		},
		Get: func(ctx context.Context, name, _ string) (any, error) {
			return client.GetTyped(ctx, apiClient, canonicalKind, v1alpha1.DefaultNamespace, name, "", newObj)
		},
		ListFunc: func(ctx context.Context) ([]any, error) {
			return listLatestAny(ctx, canonicalKind, newObj)
		},
		Delete: func(ctx context.Context, name, version string, force bool) error {
			return deleteAny(ctx, canonicalKind, name, version, force, newObj)
		},
	}
}

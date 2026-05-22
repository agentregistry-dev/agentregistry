package declarative

import (
	"context"
	"os"
	"strings"

	"github.com/spf13/cobra"

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

// apiClientMCPFetcher adapts the live registry client to mcpresolve.Fetcher
// for use by `arctl init --mcp`. The init subtree skips PersistentPreRunE
// (see pkg/cli/root.go's preRunSkipCommands), so apiClient is normally nil
// here — Fetch lazily constructs a lightweight client from the resolved
// --registry-url/--registry-token flags or their env-var defaults when that
// happens. Plain `arctl init` without --mcp stays fully offline because
// Fetch is only called when there's a ref to resolve.
type apiClientMCPFetcher struct {
	cmd *cobra.Command
}

func (f apiClientMCPFetcher) Fetch(ctx context.Context, name, tag string) (*v1alpha1.MCPServer, error) {
	c := apiClient
	if c == nil {
		c = client.NewClient(lookupRegistryURL(f.cmd), lookupRegistryToken(f.cmd))
	}
	return client.GetTyped(ctx, c, v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, name, tag, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
}

// lookupPersistentFlag walks the cmd→parent chain to find a persistent
// flag value. Needed for commands that skip PersistentPreRunE: cobra
// normally merges parent persistent flags into child flag sets at Execute
// time, but commands routed through that path won't see them. Returns
// "" if the flag isn't declared anywhere in the chain.
func lookupPersistentFlag(cmd *cobra.Command, name string) string {
	for c := cmd; c != nil; c = c.Parent() {
		if f := c.PersistentFlags().Lookup(name); f != nil {
			return f.Value.String()
		}
		if f := c.Flags().Lookup(name); f != nil {
			return f.Value.String()
		}
	}
	return ""
}

// lookupRegistryURL resolves --registry-url for commands that skip the
// root pre-run hook, falling back to env then client.DefaultBaseURL.
// Mirrors pkg/cli/root.go's resolveRegistryTarget+normalizeBaseURL.
func lookupRegistryURL(cmd *cobra.Command) string {
	raw := strings.TrimSpace(lookupPersistentFlag(cmd, "registry-url"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("ARCTL_API_BASE_URL"))
	}
	if raw == "" {
		return client.DefaultBaseURL
	}
	if !strings.HasPrefix(raw, "http://") && !strings.HasPrefix(raw, "https://") {
		raw = "http://" + raw
	}
	return raw
}

func lookupRegistryToken(cmd *cobra.Command) string {
	if v := lookupPersistentFlag(cmd, "registry-token"); v != "" {
		return v
	}
	return os.Getenv("ARCTL_API_TOKEN")
}

func init() {
	scheme.Register(typedKind(
		"agent", "agents", []string{"Agent"},
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TAG"},
			{Header: "PROVIDER"}, {Header: "MODEL"},
		},
		v1alpha1.KindAgent,
		func() *v1alpha1.Agent { return &v1alpha1.Agent{} },
		agentRow,
	))

	scheme.Register(typedKind(
		"mcp", "mcps", []string{"MCPServer", "mcpserver", "mcp-server", "mcpservers"},
		[]scheme.Column{{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindMCPServer,
		func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} },
		mcpRow,
	))

	scheme.Register(typedKind(
		"skill", "skills", []string{"Skill"},
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"},
		},
		v1alpha1.KindSkill,
		func() *v1alpha1.Skill { return &v1alpha1.Skill{} },
		skillRow,
	))

	scheme.Register(typedKind(
		"prompt", "prompts", []string{"Prompt"},
		[]scheme.Column{{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindPrompt,
		func() *v1alpha1.Prompt { return &v1alpha1.Prompt{} },
		promptRow,
	))

	// Runtime is registered manually because it is a mutable namespace/name
	// object: the server's runtime store does not expose /tags or
	// DeleteAllTags endpoints. Routing it through
	// typedKind would advertise --all-tags on its CLI surface and call
	// endpoints that don't exist. The Get / Delete / List closures match
	// what typedKind would otherwise produce; ListTags / DeleteAllTags are
	// intentionally omitted so the dispatch layer rejects --all-tags cleanly.
	scheme.Register(&scheme.Kind{
		Kind:         "runtime",
		Plural:       "runtimes",
		Aliases:      []string{"Runtime"},
		TableColumns: []scheme.Column{{Header: "NAME"}, {Header: "TYPE"}},
		ToYAMLFunc:   func(item any) any { return item },
		RowFunc: func(item any) []string {
			runtime, ok := item.(*v1alpha1.Runtime)
			if !ok {
				return []string{"<invalid>"}
			}
			return runtimeRow(runtime)
		},
		Get: func(ctx context.Context, name, _ string) (any, error) {
			return client.GetTyped(ctx, apiClient, v1alpha1.KindRuntime, v1alpha1.DefaultNamespace, name, "", func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} })
		},
		ListFunc: func(ctx context.Context, opts scheme.ListOpts) ([]any, error) {
			return listAny(ctx, v1alpha1.KindRuntime, opts, func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} })
		},
		Delete: func(ctx context.Context, name, tag string) error {
			return deleteAny(ctx, v1alpha1.KindRuntime, name, tag, func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} })
		},
	})

	// Deployment is registered manually because its Get/Delete dispatch
	// does NOT key on the v1alpha1 metadata identity (namespace/name/
	// tag). Users address deployments by the underlying target's name
	// — `arctl get deployment <agent-or-mcp-name>` — and the CLI walks the
	// /v0/deployments listing to find the matching row. The typed
	// helper assumes (kind, namespace, name, tag) lookup, which is
	// the wrong shape for this dispatch.
	scheme.Register(&scheme.Kind{
		Kind:    "deployment",
		Plural:  "deployments",
		Aliases: []string{"Deployment"},
		Get: func(ctx context.Context, name, _ string) (any, error) {
			deployment, err := client.GetTyped(ctx, apiClient, v1alpha1.KindDeployment, v1alpha1.DefaultNamespace, name, "", func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
			if err != nil {
				return nil, err
			}
			return cliCommon.DeploymentRecordFromObject(deployment), nil
		},
		Delete: func(ctx context.Context, name, tag string) error {
			return deleteAny(ctx, v1alpha1.KindDeployment, name, tag, func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} })
		},
		ListFunc: func(ctx context.Context, _ scheme.ListOpts) ([]any, error) {
			return listDeploymentAny(ctx)
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
			{Header: "NAME"}, {Header: "TARGET"}, {Header: "VERSION"},
			{Header: "TYPE"}, {Header: "RUNTIME"}, {Header: "STATUS"},
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
		Get: func(ctx context.Context, name, tag string) (any, error) {
			return client.GetTyped(ctx, apiClient, canonicalKind, v1alpha1.DefaultNamespace, name, tag, newObj)
		},
		ListFunc: func(ctx context.Context, opts scheme.ListOpts) ([]any, error) {
			return listAny(ctx, canonicalKind, opts, newObj)
		},
		Delete: func(ctx context.Context, name, tag string) error {
			return deleteAny(ctx, canonicalKind, name, tag, newObj)
		},
		ListTags: func(ctx context.Context, name string) ([]any, error) {
			return listTagsAny(ctx, canonicalKind, name, newObj)
		},
		DeleteAllTags: func(ctx context.Context, name string) error {
			return deleteAllTagsAny(ctx, canonicalKind, name, newObj)
		},
	}
}

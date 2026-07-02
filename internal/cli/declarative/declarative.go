package declarative

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	cliCommon "github.com/agentregistry-dev/agentregistry/internal/cli/common"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/client"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

// registryClientMCPFetcher adapts the root registry client to mcpresolve.Fetcher
// for use by `arctl init --mcp`. Plain `arctl init` without --mcp stays fully
// offline because Fetch is only called when there is a ref to resolve.
type registryClientMCPFetcher struct {
	cmd     *cobra.Command
	runtime cliruntime.Runtime
}

func (f registryClientMCPFetcher) Fetch(ctx context.Context, name, tag string) (*v1alpha1.MCPServer, error) {
	if f.runtime == nil {
		return nil, fmt.Errorf("registry runtime not configured")
	}
	c, err := f.runtime.RegistryClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving registry client: %w", err)
	}
	return client.GetTyped(ctx, c, v1alpha1.KindMCPServer, v1alpha1.DefaultNamespace, name, tag, func() *v1alpha1.MCPServer { return &v1alpha1.MCPServer{} })
}

// lookupPersistentFlag walks the cmd→parent chain to find a persistent flag
// value. Returns "" if the flag is not declared anywhere in the chain.
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
	scheme.Register(
		mutableTypedKind(
			"runtime", "runtimes", []string{"Runtime"},
			[]scheme.Column{{Header: "NAME"}, {Header: "TYPE"}},
			v1alpha1.KindRuntime,
			func() *v1alpha1.Runtime { return &v1alpha1.Runtime{} },
			runtimeRow,
		),
	)

	// Deployment is registered manually because it is a mutable namespace/name
	// object: the server's deployment store does not expose /tags or
	// DeleteAllTags endpoints. Explicit get/delete accept either NAME or
	// NAMESPACE/NAME; ListTags / DeleteAllTags are intentionally omitted so
	// the dispatch layer rejects --all-tags cleanly.
	scheme.Register(
		mutableTypedKind(
			"deployment", "deployments", []string{"Deployment"},
			[]scheme.Column{
				{Header: "NAME"}, {Header: "TARGET"}, {Header: "VERSION"},
				{Header: "TYPE"}, {Header: "RUNTIME"}, {Header: "STATUS"},
			},
			v1alpha1.KindDeployment,
			func() *v1alpha1.Deployment { return &v1alpha1.Deployment{} },
			func(deployment *v1alpha1.Deployment) []string {
				return deploymentRow(cliCommon.DeploymentRecordFromObject(deployment))
			},
			withMutableListFunc(listDeploymentResources),
		),
	)
}

// typedKind builds a scheme.Kind whose Get / List / Delete dispatch
// closures all wire through the typed v1alpha1 client helpers
// (client.GetTyped[T] / client.ListAllTyped[T] / client.Delete) for
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
		Get: func(ctx context.Context, c *client.Client, name, tag string) (any, error) {
			ref, err := parseResourceLookupRef(name)
			if err != nil {
				return nil, err
			}
			return client.GetTyped(ctx, c, canonicalKind, ref.Namespace, ref.Name, tag, newObj)
		},
		ListFunc: func(ctx context.Context, c *client.Client, opts scheme.ListOpts) ([]any, error) {
			return listAny(ctx, c, canonicalKind, opts, newObj)
		},
		Delete: func(ctx context.Context, c *client.Client, name, tag string) error {
			return deleteAny(ctx, c, canonicalKind, name, tag, newObj)
		},
		ListTags: func(ctx context.Context, c *client.Client, name string) ([]any, error) {
			return listTagsAny(ctx, c, canonicalKind, name, newObj)
		},
		DeleteAllTags: func(ctx context.Context, c *client.Client, name string) error {
			return deleteAllTagsAny(ctx, c, canonicalKind, name, newObj)
		},
	}
}

// mutableTypedKind is typedKind for namespace/name resources such as Runtime
// and Deployment. These kinds use the generic v1alpha1 CRUD surface but do not
// expose /tags, so the tag-specific callbacks are intentionally nil.
type mutableTypedKindOption func(*scheme.Kind)

// withMutableListFunc is an option to override the default ListFunc for a
// mutableTypedKind.
func withMutableListFunc(fn scheme.ListFunc) mutableTypedKindOption {
	return func(k *scheme.Kind) {
		k.ListFunc = fn
	}
}

// mutableTypedKind builds a scheme.Kind for mutable namespace/name resources which
// do not support tagging.
func mutableTypedKind[T v1alpha1.Object](
	cliName, plural string,
	aliases []string,
	columns []scheme.Column,
	canonicalKind string,
	newObj func() T,
	row func(T) []string,
	opts ...mutableTypedKindOption,
) *scheme.Kind {
	k := &scheme.Kind{
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
		Get: func(ctx context.Context, c *client.Client, name, _ string) (any, error) {
			ref, err := parseResourceLookupRef(name)
			if err != nil {
				return nil, err
			}
			return client.GetTyped(ctx, c, canonicalKind, ref.Namespace, ref.Name, "", newObj)
		},
		ListFunc: func(ctx context.Context, c *client.Client, opts scheme.ListOpts) ([]any, error) {
			return listAny(ctx, c, canonicalKind, opts, newObj)
		},
		Delete: func(ctx context.Context, c *client.Client, name, tag string) error {
			return deleteAny(ctx, c, canonicalKind, name, tag, newObj)
		},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(k)
		}
	}
	return k
}

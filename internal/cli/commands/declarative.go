package commands

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
	registerKind[*v1alpha1.Agent](
		"agent", "", nil,
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TAG"}, {Header: "MODE"}, {Header: "DESCRIPTION"},
		},
		v1alpha1.KindAgent,
		agentRow,
	)
	registerKind[*v1alpha1.MCPServer](
		"mcp", "mcps", []string{"MCPServer", "mcpserver", "mcp-server", "mcpservers"},
		[]scheme.Column{{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindMCPServer,
		mcpRow,
	)
	registerKind[*v1alpha1.Skill](
		"skill", "", nil,
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"},
		},
		v1alpha1.KindSkill,
		skillRow,
	)
	registerKind[*v1alpha1.Prompt](
		"prompt", "", nil,
		[]scheme.Column{{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindPrompt,
		promptRow,
	)
	registerKind[*v1alpha1.Plugin](
		"plugin", "", nil,
		[]scheme.Column{{Header: "NAME"}, {Header: "TAG"}, {Header: "DESCRIPTION"}},
		v1alpha1.KindPlugin,
		pluginRow,
	)
	registerKind[*v1alpha1.Model](
		"model", "", nil,
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TAG"}, {Header: "PROVIDER"},
			{Header: "MODEL"}, {Header: "AUTH"},
		},
		v1alpha1.KindModel,
		modelRow,
	)

	registerKind[*v1alpha1.Runtime](
		"runtime", "", nil,
		[]scheme.Column{{Header: "NAME"}, {Header: "TYPE"}, {Header: "STATUS"}},
		v1alpha1.KindRuntime,
		runtimeRow,
	)
	registerKind[*v1alpha1.Secret](
		"secret", "", nil,
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TYPE"}, {Header: "KEYS"}, {Header: "IMMUTABLE"},
		},
		v1alpha1.KindSecret,
		secretRow,
	)
	registerKind[*v1alpha1.Deployment](
		"deployment", "", nil,
		[]scheme.Column{
			{Header: "NAME"}, {Header: "TARGET"}, {Header: "VERSION"},
			{Header: "TYPE"}, {Header: "RUNTIME"}, {Header: "STATUS"},
		},
		v1alpha1.KindDeployment,
		func(deployment *v1alpha1.Deployment) []string {
			return deploymentRow(cliCommon.DeploymentRecordFromObject(deployment))
		},
		withListFunc(listDeploymentResources),
	)
}

type kindOption func(*scheme.Kind)

func withListFunc(fn scheme.ListFunc) kindOption {
	return func(k *scheme.Kind) {
		k.ListFunc = fn
	}
}

func registerKind[T v1alpha1.Object](
	cliName, plural string,
	aliases []string,
	columns []scheme.Column,
	canonicalKind string,
	row func(T) []string,
	opts ...kindOption,
) {
	descriptor, ok := v1alpha1.KindDescriptorFor(canonicalKind)
	if !ok {
		panic("commands.registerKind: v1alpha1 kind is not registered: " + canonicalKind)
	}
	if obj := descriptor.NewObject(); obj == nil {
		panic("commands.registerKind: constructor for " + canonicalKind + " returned nil")
	} else if _, ok := obj.(T); !ok {
		panic(fmt.Sprintf("commands.registerKind: constructor for %s returned %T", canonicalKind, obj))
	}
	newObj := func() v1alpha1.Object {
		obj, ok := descriptor.NewObject().(T)
		if !ok {
			panic(fmt.Sprintf("commands.registerKind: constructor for %s returned %T", canonicalKind, obj))
		}
		return obj
	}
	rowObject := func(obj v1alpha1.Object) []string {
		t, ok := obj.(T)
		if !ok {
			return []string{"<invalid>"}
		}
		return row(t)
	}
	scheme.Register(newKindFromDescriptor(
		descriptor,
		cliName,
		plural,
		aliases,
		columns,
		newObj,
		rowObject,
		opts...,
	))
}

func newKindFromDescriptor(
	descriptor v1alpha1.KindDescriptor,
	cliName, plural string,
	aliases []string,
	columns []scheme.Column,
	newObj func() v1alpha1.Object,
	row func(v1alpha1.Object) []string,
	opts ...kindOption,
) *scheme.Kind {
	if plural == "" {
		plural = descriptor.Plural
	}
	if newObj == nil {
		newObj = func() v1alpha1.Object {
			obj, ok := descriptor.NewObject().(v1alpha1.Object)
			if !ok {
				panic(fmt.Sprintf("commands: constructor for %s returned %T", descriptor.Kind, obj))
			}
			return obj
		}
	}
	if obj := newObj(); obj == nil {
		panic("commands: constructor for " + descriptor.Kind + " returned nil")
	}
	if row == nil {
		row = func(obj v1alpha1.Object) []string {
			return []string{obj.GetMetadata().Name}
		}
	}
	tagged := descriptor.Storage == v1alpha1.KindStorageTaggedArtifact
	k := &scheme.Kind{
		Kind:         cliName,
		Plural:       plural,
		Aliases:      aliases,
		TableColumns: columns,
		ToYAMLFunc:   func(item any) any { return item },
		RowFunc: func(item any) []string {
			obj, ok := item.(v1alpha1.Object)
			if !ok {
				return []string{"<invalid>"}
			}
			return row(obj)
		},
		Get: func(ctx context.Context, c *client.Client, name, tag string) (any, error) {
			ref, err := parseResourceLookupRef(name)
			if err != nil {
				return nil, err
			}
			if !tagged {
				tag = ""
			}
			return client.GetTyped(ctx, c, descriptor.Kind, ref.Namespace, ref.Name, tag, newObj)
		},
		ListFunc: func(ctx context.Context, c *client.Client, opts scheme.ListOpts) ([]any, error) {
			return listAny(ctx, c, descriptor.Kind, opts, newObj)
		},
		Delete: func(ctx context.Context, c *client.Client, name, tag string) error {
			return deleteAny(ctx, c, descriptor.Kind, name, tag, tagged, newObj)
		},
	}
	if tagged {
		k.ListTags = func(ctx context.Context, c *client.Client, name string) ([]any, error) {
			return listTagsAny(ctx, c, descriptor.Kind, name, newObj)
		}
		k.DeleteAllTags = func(ctx context.Context, c *client.Client, name string) error {
			return deleteAllTagsAny(ctx, c, descriptor.Kind, name, newObj)
		}
	}
	for _, opt := range opts {
		if opt != nil {
			opt(k)
		}
	}
	return k
}

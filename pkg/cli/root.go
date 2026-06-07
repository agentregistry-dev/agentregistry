package cli

import (
	"github.com/spf13/cobra"

	internalcli "github.com/agentregistry-dev/agentregistry/internal/cli"
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
	clidaemon "github.com/agentregistry-dev/agentregistry/internal/cli/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/db"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
	"github.com/agentregistry-dev/agentregistry/pkg/daemon/dockercompose"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/legacymigrate"
)

const (
	defaultUse   = "arctl"
	defaultShort = "Agent Registry CLI"
	defaultLong  = "arctl is a CLI tool for managing agents, MCP servers, skills, and prompts."
)

// Root creates a fresh arctl root command from Config.
func Root(cfg Config) *cobra.Command {
	cfg = cfg.withDefaults()

	root := &cobra.Command{
		Use:   cfg.Use,
		Short: cfg.Short,
		Long:  cfg.Long,
	}
	var registryURL string
	var registryToken string
	rt := cliruntime.New(cliruntime.Config{
		Env:             cfg.Env,
		Auth:            cfg.Auth,
		RegistryURL:     &registryURL,
		RegistryToken:   &registryToken,
		OnTokenResolved: cfg.OnTokenResolved,
	})
	root.PersistentFlags().StringVar(&registryURL, "registry-url", cfg.Env.Getenv("ARCTL_API_BASE_URL"), "Registry URL (overrides ARCTL_API_BASE_URL env var; defaults to http://localhost:12121)")
	root.PersistentFlags().StringVar(&registryToken, "registry-token", "", "Registry bearer token (defaults to value of ARCTL_API_TOKEN env var)")

	kinds := scheme.NewRegistry(scheme.All()...)
	for _, kind := range cfg.DeclarativeKinds {
		if kind.Name == "" {
			panic("registering declarative kind: name is required")
		}
		columns := make([]scheme.Column, 0, len(kind.TableColumns))
		for _, header := range kind.TableColumns {
			columns = append(columns, scheme.Column{Header: header})
		}
		kinds.Register(declarative.NewExtensionKind(declarative.ExtensionKind{
			Name:          kind.Name,
			Plural:        kind.Plural,
			CanonicalKind: kind.CanonicalKind,
			Aliases:       kind.Aliases,
			TableColumns:  columns,
			NewObject:     kind.NewObject,
			Row:           kind.Row,
		}))
	}

	deps := cliruntime.Deps{
		Runtime: rt,
		Auth:    cfg.Auth,
		Kinds:   kinds,
	}
	addCommand := func(id string, cmd *cobra.Command) {
		if cfg.Disabled[id] || cmd == nil {
			return
		}
		root.AddCommand(cmd)
	}

	addCommand(cliruntime.CommandConfigure, configure.NewCommand(deps))
	addCommand(cliruntime.CommandVersion, internalcli.NewVersionCommand(deps))
	addCommand(cliruntime.CommandDaemon, clidaemon.NewCommand(dockercompose.NewManager(dockercompose.DefaultConfig())))
	addCommand(cliruntime.CommandApply, declarative.NewApplyCmd(deps))
	addCommand(cliruntime.CommandGet, declarative.NewGetCmd(deps))
	addCommand(cliruntime.CommandDelete, declarative.NewDeleteCmd(deps))
	addCommand(cliruntime.CommandInit, declarative.NewInitCmd(deps))
	addCommand(cliruntime.CommandBuild, declarative.NewBuildCmd(deps))
	addCommand(cliruntime.CommandRun, declarative.NewRunCmd(deps))
	addCommand(cliruntime.CommandPull, declarative.NewPullCmd(deps))
	addCommand(cliruntime.CommandWait, declarative.NewWaitCmd(deps))
	addCommand(cliruntime.CommandDB, db.NewCommand(legacymigrate.OSSSource()))

	for _, cmd := range cfg.ExtraCommands {
		if cmd == nil {
			continue
		}
		root.AddCommand(cmd)
	}

	return root
}

// Config describes one CLI instance.
type Config struct {
	Use   string
	Short string
	Long  string

	Env  cliruntime.Env
	Auth cliruntime.AuthProvider

	ExtraCommands []*cobra.Command
	Disabled      map[string]bool

	DeclarativeKinds []DeclarativeKind

	OnTokenResolved func(token string) error
}

// DeclarativeKind describes a downstream v1alpha1 kind exposed through generic
// get, list, and delete dispatch.
type DeclarativeKind struct {
	Name          string
	Plural        string
	CanonicalKind string
	Aliases       []string
	TableColumns  []string
	NewObject     func() v1alpha1.Object
	Row           func(v1alpha1.Object) []string
}

func DefaultConfig() Config {
	return Config{
		Use:      defaultUse,
		Short:    defaultShort,
		Long:     defaultLong,
		Env:      cliruntime.OSEnv{},
		Auth:     cliruntime.NoopAuthProvider{},
		Disabled: map[string]bool{},
	}
}

func (c Config) withDefaults() Config {
	if c.Use == "" {
		c.Use = defaultUse
	}
	if c.Short == "" {
		c.Short = defaultShort
	}
	if c.Long == "" {
		c.Long = defaultLong
	}
	if c.Env == nil {
		c.Env = cliruntime.OSEnv{}
	}
	if c.Auth == nil {
		c.Auth = cliruntime.NoopAuthProvider{}
	}
	if c.Disabled == nil {
		c.Disabled = map[string]bool{}
	}
	return c
}

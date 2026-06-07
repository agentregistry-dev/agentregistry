package cli

import (
	"strings"

	"github.com/spf13/cobra"

	internalcli "github.com/agentregistry-dev/agentregistry/internal/cli"
	"github.com/agentregistry-dev/agentregistry/internal/cli/configure"
	clidaemon "github.com/agentregistry-dev/agentregistry/internal/cli/daemon"
	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/agentregistry-dev/agentregistry/internal/version"
	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/db"
	"github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
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
		Use:     cfg.Use,
		Short:   cfg.Short,
		Long:    cfg.Long,
		Version: cfg.Version,
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
	root.AddCommand(configure.NewCommand(deps))
	root.AddCommand(internalcli.NewVersionCommand(deps))
	root.AddCommand(clidaemon.NewCommand(dockercompose.NewManager(dockercompose.DefaultConfig())))
	root.AddCommand(declarative.NewApplyCmd(deps))
	root.AddCommand(declarative.NewGetCmd(deps))
	root.AddCommand(declarative.NewDeleteCmd(deps))
	root.AddCommand(declarative.NewInitCmd(deps))
	root.AddCommand(declarative.NewBuildCmd(deps))
	root.AddCommand(declarative.NewRunCmd(deps))
	root.AddCommand(declarative.NewPullCmd(deps))
	root.AddCommand(declarative.NewWaitCmd(deps))
	migrationSources := append([]migrate.Source{legacymigrate.OSSSource()}, cfg.ExtraMigrationSources...)
	root.AddCommand(db.NewCommand(migrationSources...))

	removeDisabledCommands(root, cfg.Disabled)

	for _, cmd := range cfg.ExtraCommands {
		if cmd == nil {
			continue
		}
		root.AddCommand(cmd)
	}

	return root
}

func removeDisabledCommands(root *cobra.Command, disabled map[string]bool) {
	for path, disabled := range disabled {
		if !disabled {
			continue
		}
		parts := strings.Fields(path)
		if len(parts) == 0 {
			continue
		}

		parent := root
		for i, part := range parts {
			var match *cobra.Command
			for _, cmd := range parent.Commands() {
				if cmd.Name() == part {
					match = cmd
					break
				}
			}
			if match == nil {
				break
			}
			if i == len(parts)-1 {
				parent.RemoveCommand(match)
				break
			}
			parent = match
		}
	}
}

// Config describes one CLI instance.
type Config struct {
	Use     string
	Short   string
	Long    string
	Version string

	Env  cliruntime.Env
	Auth cliruntime.AuthProvider

	ExtraCommands []*cobra.Command
	Disabled      map[string]bool // command paths to remove, such as "daemon" or "db migrate goto"

	ExtraMigrationSources []migrate.Source
	DeclarativeKinds      []DeclarativeKind

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
		Version:  version.Version,
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
	if c.Version == "" {
		c.Version = version.Version
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

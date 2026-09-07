package cli

import (
	"testing"

	"github.com/spf13/cobra"

	dbmigrate "github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
	cliruntime "github.com/agentregistry-dev/agentregistry/pkg/cli/runtime"
)

func TestRootExtraCommandsReceiveDeps(t *testing.T) {
	var (
		factoryCalls int
		gotDeps      cliruntime.Deps
		gotTarget    cliruntime.RegistryTarget
	)
	cfg := DefaultConfig()
	cfg.ExtraCommands = func(deps cliruntime.Deps) []*cobra.Command {
		factoryCalls++
		gotDeps = deps
		return []*cobra.Command{{
			Use: "extension",
			RunE: func(cmd *cobra.Command, _ []string) error {
				var err error
				gotTarget, err = deps.Runtime.ResolveRegistryTarget(cmd.Context())
				return err
			},
		}}
	}

	root := Root(cfg)
	root.SetArgs([]string{
		"--registry-url", "registry.example.com",
		"--registry-token", "flag-token",
		"extension",
	})
	if err := root.Execute(); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if factoryCalls != 1 {
		t.Fatalf("ExtraCommands() calls = %d, want 1", factoryCalls)
	}
	if gotDeps.Runtime == nil {
		t.Fatal("ExtraCommands() received nil Runtime")
	}
	if gotDeps.Auth != cfg.Auth {
		t.Fatal("ExtraCommands() received a different Auth provider")
	}
	if gotDeps.Kinds == nil {
		t.Fatal("ExtraCommands() received nil Kinds registry")
	}
	wantTarget := cliruntime.RegistryTarget{
		BaseURL: "http://registry.example.com",
		Token:   "flag-token",
	}
	if gotTarget != wantTarget {
		t.Fatalf("resolved target = %#v, want %#v", gotTarget, wantTarget)
	}
}

func TestRootDisabledCommandPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Disabled["db migrate goto"] = true
	cfg.ExtraMigrationSources = []dbmigrate.Source{{Name: "extension"}}

	root := Root(cfg)

	dbCmd := childCommand(root, "db")
	if dbCmd == nil {
		t.Fatal("expected db command")
	}
	migrateCmd := childCommand(dbCmd, "migrate")
	if migrateCmd == nil {
		t.Fatal("expected db migrate command")
	}
	if got := childCommand(migrateCmd, "goto"); got != nil {
		t.Fatalf("db migrate goto was not disabled: %#v", got)
	}
	if migrateCmd.PersistentFlags().Lookup("source") == nil {
		t.Fatal("expected db migrate --source flag for multiple migration sources")
	}
}

func childCommand(parent *cobra.Command, name string) *cobra.Command {
	for _, cmd := range parent.Commands() {
		if cmd.Name() == name {
			return cmd
		}
	}
	return nil
}

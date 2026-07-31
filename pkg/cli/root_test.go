package cli

import (
	"testing"

	"github.com/spf13/cobra"

	dbmigrate "github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
)

func TestRootDisabledCommandPaths(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Disabled["db migrate goto"] = true
	cfg.ExtraMigrationSources = []dbmigrate.Source{{Name: "enterprise"}}

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

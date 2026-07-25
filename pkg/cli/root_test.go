package cli

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/agentregistry-dev/agentregistry/internal/version"
	dbmigrate "github.com/agentregistry-dev/agentregistry/pkg/cli/db/migrate"
)

func TestRootDisabledCommandPathsPruneBuiltInsBeforeExtraCommands(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Disabled["daemon"] = true
	cfg.Disabled["db migrate goto"] = true
	cfg.ExtraCommands = []*cobra.Command{{Use: "daemon", Short: "Enterprise daemon command"}}
	cfg.ExtraMigrationSources = []dbmigrate.Source{{Name: "enterprise"}}

	root := Root(cfg)

	daemon := childCommand(root, "daemon")
	if daemon == nil {
		t.Fatal("expected extra daemon command to be registered")
	}
	if daemon.Short != "Enterprise daemon command" {
		t.Fatalf("daemon.Short = %q, want extra command", daemon.Short)
	}

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

type testEnv map[string]string

func (e testEnv) Getenv(key string) string {
	return e[key]
}

func TestDaemonManagerConfigUsesEnvOverride(t *testing.T) {
	const override = "registry.example.com"

	cfg := DefaultConfig()
	cfg.Env = testEnv{
		"ARCTL_DAEMON_DOCKER_REGISTRY": "  " + override + "  ",
	}

	daemonConfig := daemonManagerConfig(cfg)
	if daemonConfig.DockerRegistry != override {
		t.Fatalf("daemonManagerConfig() DockerRegistry = %q, want %q", daemonConfig.DockerRegistry, override)
	}
}

func TestDaemonManagerConfigFallsBackToDefaultRegistry(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Env = testEnv{}

	daemonConfig := daemonManagerConfig(cfg)
	if daemonConfig.DockerRegistry != version.DockerRegistry {
		t.Fatalf("daemonManagerConfig() DockerRegistry = %q, want %q", daemonConfig.DockerRegistry, version.DockerRegistry)
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

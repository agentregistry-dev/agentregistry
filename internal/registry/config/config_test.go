package config

import (
	"os"
	"testing"
	"time"
)

func TestNewConfig_ControllerEnv(t *testing.T) {
	t.Setenv("AGENT_REGISTRY_CONTROLLER_EVENT_RETENTION", "2h")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_EVENT_KEEP_AFTER_REVISION", "42")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_RETENTION_PRUNE_BATCH_LIMIT", "17")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_DISCOVERY_INTERVAL", "15s")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_DISCOVERY_STALE_AFTER_MISSES", "2")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_DISCOVERY_DELETE_AFTER_MISSES", "4")

	cfg := NewConfig()

	if cfg.ControllerEventRetention != 2*time.Hour {
		t.Fatalf("event retention = %s, want 2h", cfg.ControllerEventRetention)
	}
	if cfg.ControllerEventKeepAfterRevision != 42 {
		t.Fatalf("keep-after revision = %d, want 42", cfg.ControllerEventKeepAfterRevision)
	}
	if cfg.ControllerRetentionPruneBatchLimit != 17 {
		t.Fatalf("prune batch limit = %d, want 17", cfg.ControllerRetentionPruneBatchLimit)
	}
	if cfg.ControllerDiscoveryInterval != 15*time.Second {
		t.Fatalf("discovery interval = %s, want 15s", cfg.ControllerDiscoveryInterval)
	}
	if cfg.ControllerDiscoveryStaleAfterMisses != 2 {
		t.Fatalf("discovery stale misses = %d, want 2", cfg.ControllerDiscoveryStaleAfterMisses)
	}
	if cfg.ControllerDiscoveryDeleteAfterMisses != 4 {
		t.Fatalf("discovery delete misses = %d, want 4", cfg.ControllerDiscoveryDeleteAfterMisses)
	}
}

func TestNewConfig_SecretStoreEnv(t *testing.T) {
	t.Setenv("AGENT_REGISTRY_SECRET_STORE", "Kubernetes")
	t.Setenv("AGENT_REGISTRY_SECRET_STORE_KUBERNETES_NAMESPACE", "custom-system")

	cfg := NewConfig()

	if cfg.SecretStore != "Kubernetes" {
		t.Fatalf("SecretStore = %q, want Kubernetes", cfg.SecretStore)
	}
	if cfg.SecretStoreNamespace != "custom-system" {
		t.Fatalf("SecretStoreNamespace = %q, want custom-system", cfg.SecretStoreNamespace)
	}
}

func TestNewConfig_SecretStoreNamespaceDefault(t *testing.T) {
	if got := NewConfig().SecretStoreNamespace; got != "agentregistry-system" {
		t.Fatalf("SecretStoreNamespace = %q, want agentregistry-system", got)
	}
}

func TestNewConfig_SkipMigrationsEnv(t *testing.T) {
	cases := []struct {
		name string
		bare string // value of SKIP_MIGRATIONS; "" means unset
		want bool
	}{
		{"unset", "", false},
		{"true", "true", true},
		{"false explicit", "false", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// The prefixed form is no longer supported; setting it must
			// have no effect on the gate.
			t.Setenv("AGENT_REGISTRY_SKIP_MIGRATIONS", "true")
			os.Unsetenv("SKIP_MIGRATIONS")
			if tc.bare != "" {
				t.Setenv("SKIP_MIGRATIONS", tc.bare)
			}
			cfg := NewConfig()
			if cfg.SkipMigrations != tc.want {
				t.Fatalf("SkipMigrations = %v; want %v (bare=%q)", cfg.SkipMigrations, tc.want, tc.bare)
			}
		})
	}
}

package config

import (
	"os"
	"strings"
	"testing"
)

func TestNewConfig_RuntimeDirHasRandomSuffix(t *testing.T) {
	// Ensure the env var is unset so the default path is used.
	os.Unsetenv("AGENT_REGISTRY_RUNTIME_DIR")

	cfg := NewConfig()

	base := "/tmp/arctl-runtime-"
	if !strings.HasPrefix(cfg.RuntimeDir, base) {
		t.Fatalf("RuntimeDir should start with %q, got %q", base, cfg.RuntimeDir)
	}

	suffix := strings.TrimPrefix(cfg.RuntimeDir, base)
	if len(suffix) != 16 { // 8 bytes = 16 hex chars
		t.Fatalf("RuntimeDir suffix should be 16 hex chars, got %q (len %d)", suffix, len(suffix))
	}
}

func TestNewConfig_RuntimeDirUniqueBetweenCalls(t *testing.T) {
	os.Unsetenv("AGENT_REGISTRY_RUNTIME_DIR")

	cfg1 := NewConfig()
	cfg2 := NewConfig()

	if cfg1.RuntimeDir == cfg2.RuntimeDir {
		t.Fatalf("two NewConfig() calls should produce different RuntimeDir values, both got %q", cfg1.RuntimeDir)
	}
}

func TestNewConfig_RuntimeDirRespectsEnvOverride(t *testing.T) {
	custom := "/custom/runtime/path"
	t.Setenv("AGENT_REGISTRY_RUNTIME_DIR", custom)

	cfg := NewConfig()

	if cfg.RuntimeDir != custom {
		t.Fatalf("RuntimeDir should be %q when env var is set, got %q", custom, cfg.RuntimeDir)
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

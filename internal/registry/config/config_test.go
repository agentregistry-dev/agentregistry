package config

import (
	"os"
	"strings"
	"testing"
	"time"
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

func TestNewConfig_ControllerRetentionEnv(t *testing.T) {
	t.Setenv("AGENT_REGISTRY_RUNTIME_DIR", "/tmp/runtime")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_EVENT_RETENTION", "2h")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_EVENT_KEEP_AFTER_REVISION", "42")
	t.Setenv("AGENT_REGISTRY_CONTROLLER_RETENTION_PRUNE_BATCH_LIMIT", "17")

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
}

package migrate

import (
	"bytes"
	"context"
	"embed"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/jackc/pgx/v5"
)

//go:embed testdata/range_fixture/*.sql
var rangeFixtureFiles embed.FS

func TestSourceBoundsFromConfig(t *testing.T) {
	cfg := database.MigratorConfig{
		MigrationFiles: rangeFixtureFiles,
		MigrationDir:   "testdata/range_fixture",
		VersionOffset:  200,
	}
	low, high := sourceBoundsFromConfig(cfg)
	// Fixture contains migrations 001, 002, 003. Offset 200 → range [201, 203].
	if low != 201 || high != 203 {
		t.Fatalf("expected bounds [201, 203]; got [%d, %d]", low, high)
	}
}

func TestSourceBoundsFromConfig_EmptyDir(t *testing.T) {
	cfg := database.MigratorConfig{
		MigrationFiles: rangeFixtureFiles,
		MigrationDir:   "testdata/does_not_exist",
		VersionOffset:  500,
	}
	low, high := sourceBoundsFromConfig(cfg)
	// Missing dir returns the empty-range sentinel (low, low-1) so the
	// CLI's `if v >= low && v <= high` routing checks match no version
	// against an empty source.
	if low != 501 || high != 500 {
		t.Fatalf("expected empty-range sentinel [501, 500] for missing dir; got [%d, %d]", low, high)
	}
}

func TestSourceBoundsFromConfig_HonorsSkip(t *testing.T) {
	cfg := database.MigratorConfig{
		MigrationFiles: rangeFixtureFiles,
		MigrationDir:   "testdata/range_fixture",
		VersionOffset:  200,
		Skip:           func(v int) bool { return v == 3 },
	}
	low, high := sourceBoundsFromConfig(cfg)
	// 003 skipped → highest visible version is 2.
	if low != 201 || high != 202 {
		t.Fatalf("expected bounds [201, 202] with 003 skipped; got [%d, %d]", low, high)
	}
}

func TestDownCmd_RejectsNonPositiveN(t *testing.T) {
	// Negative ints are filtered by cobra at the flag-parsing layer
	// (interpreted as shorthand flags); the RunE-level check guards
	// against zero and non-numeric values.
	cases := []string{"0", "abc"}
	for _, arg := range cases {
		t.Run(arg, func(t *testing.T) {
			cmd := newDownCmd()
			cmd.SetArgs([]string{arg})
			cmd.SetOut(&bytes.Buffer{})
			cmd.SetErr(&bytes.Buffer{})
			cmd.SetContext(context.Background())
			err := cmd.Execute()
			if err == nil {
				t.Fatalf("expected error for arg %q", arg)
			}
			if !strings.Contains(err.Error(), "positive integer") {
				t.Fatalf("error message should mention positive integer: %v", err)
			}
		})
	}
}

func TestForceCmd_RejectsNonPositiveV(t *testing.T) {
	cmd := newForceCmd()
	cmd.SetArgs([]string{"0"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for V=0")
	}
	if !strings.Contains(err.Error(), "positive integer") {
		t.Fatalf("error message should mention positive integer: %v", err)
	}
}

func TestGotoCmd_RejectsNegativeV(t *testing.T) {
	cmd := newGotoCmd()
	cmd.SetArgs([]string{"-5"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error for V=-5")
	}
}

func TestGotoCmd_AcceptsZero(t *testing.T) {
	// goto 0 is the special "empty schema" target; the arg-validation
	// layer must let it through. With no DSN set the command stops at
	// withConn, so we use that as a positive signal that arg parsing
	// accepted the input.
	t.Setenv(dbURLEnv, "")
	flags.dbURL = ""
	cmd := newGotoCmd()
	cmd.SetArgs([]string{"0"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetContext(context.Background())
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected withConn DSN error for V=0; got nil")
	}
	if !strings.Contains(err.Error(), dbURLEnv) {
		t.Fatalf("V=0 should reach withConn; got non-DSN error: %v", err)
	}
}

func TestWithConn_NoDSN(t *testing.T) {
	t.Setenv(dbURLEnv, "")
	flags.dbURL = ""
	err := withConn(context.Background(), func(_ context.Context, _ *pgx.Conn) error {
		t.Fatal("should not be invoked when DSN is missing")
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), dbURLEnv) {
		t.Fatalf("expected error mentioning %s; got %v", dbURLEnv, err)
	}
}

func TestEmbeddingsEnabledOverride(t *testing.T) {
	// Save & restore package globals; NewCommand sets migrateCmd.
	prevCmd := migrateCmd
	prevVal := flags.embeddingsEnabled
	t.Cleanup(func() {
		migrateCmd = prevCmd
		flags.embeddingsEnabled = prevVal
	})

	t.Run("nil command before NewCommand", func(t *testing.T) {
		migrateCmd = nil
		flags.embeddingsEnabled = true
		val, set := EmbeddingsEnabledOverride()
		if set || val {
			t.Fatalf("expected (false, false) when migrateCmd is nil; got (%v, %v)", val, set)
		}
	})

	t.Run("not passed", func(t *testing.T) {
		_ = NewCommand()
		val, set := EmbeddingsEnabledOverride()
		if set || val {
			t.Fatalf("expected (false, false) when --embeddings-enabled is not passed; got (%v, %v)", val, set)
		}
	})

	t.Run("passed true", func(t *testing.T) {
		cmd := NewCommand()
		if err := cmd.PersistentFlags().Set(embeddingsFlag, "true"); err != nil {
			t.Fatalf("setting flag: %v", err)
		}
		val, set := EmbeddingsEnabledOverride()
		if !set || !val {
			t.Fatalf("expected (true, true) after --embeddings-enabled=true; got (%v, %v)", val, set)
		}
	})

	t.Run("passed false", func(t *testing.T) {
		cmd := NewCommand()
		if err := cmd.PersistentFlags().Set(embeddingsFlag, "false"); err != nil {
			t.Fatalf("setting flag: %v", err)
		}
		val, set := EmbeddingsEnabledOverride()
		if !set || val {
			t.Fatalf("expected (false, true) after --embeddings-enabled=false; got (%v, %v)", val, set)
		}
	})
}

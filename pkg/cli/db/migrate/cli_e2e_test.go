//go:build e2e

// CLI e2e tests for `arctl db migrate`. Each test builds the actual
// arctl binary (once, cached for the test run) and execs it as a
// subprocess. The DB-required cases create a fresh per-test Postgres
// database against localhost:5432 (matching the existing integration
// pattern) and skip when Postgres is unavailable; the no-DB cases
// always run.
//
// Run with: make test-e2e-cli  (or `go test -tags=e2e ./pkg/cli/db/migrate/...`).

package migrate_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---- binary build (lazy, once per `go test` invocation) ----

var (
	arctlBinPath   string
	arctlBuildErr  error
	arctlBuildOnce sync.Once
)

func arctlBin(t *testing.T) string {
	t.Helper()
	arctlBuildOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			arctlBuildErr = errors.New("could not determine test file location")
			return
		}
		// pkg/cli/db/migrate/cli_e2e_test.go → up 4 levels = repo root.
		repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", "..", "..", ".."))

		tmpDir, err := os.MkdirTemp("", "arctl-e2e-")
		if err != nil {
			arctlBuildErr = fmt.Errorf("mkdtemp: %w", err)
			return
		}
		binPath := filepath.Join(tmpDir, "arctl")
		cmd := exec.Command("go", "build", "-o", binPath, "./cmd/cli")
		cmd.Dir = repoRoot
		if out, berr := cmd.CombinedOutput(); berr != nil {
			_ = os.RemoveAll(tmpDir)
			arctlBuildErr = fmt.Errorf("go build ./cmd/cli: %w\noutput:\n%s", berr, out)
			return
		}
		arctlBinPath = binPath
		// tmpDir is intentionally leaked (OS cleans /tmp eventually);
		// registering t.Cleanup on the first caller would remove the
		// binary before the rest of the test run could use it.
	})
	if arctlBuildErr != nil {
		t.Fatalf("build arctl: %v", arctlBuildErr)
	}
	return arctlBinPath
}

// ---- exec helper ----

type arctlResult struct {
	stdout   string
	stderr   string
	combined string // stderr + stdout, for substring assertions when the
	// CLI mixes the two (cobra prints "Error: ..." to stderr but
	// some operators / wrappers swap streams).
	exitCode int
}

func runArctl(t *testing.T, env []string, args ...string) arctlResult {
	t.Helper()
	bin := arctlBin(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, args...)
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		exitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("running arctl %v: %v", args, err)
	}
	return arctlResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		combined: stderr.String() + stdout.String(),
		exitCode: exitCode,
	}
}

// ---- fresh per-test DB (skips when Postgres is unavailable) ----

const adminURI = "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"

// freshDB returns a DSN to a brand-new database, dropped on test
// cleanup. Skips the test when localhost:5432 is unreachable so the
// cheap tier doesn't depend on Postgres being up.
func freshDB(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)

	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available at localhost:5432: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	var randomBytes [8]byte
	if _, rerr := rand.Read(randomBytes[:]); rerr != nil {
		t.Fatalf("rand.Read: %v", rerr)
	}
	dbName := fmt.Sprintf("test_e2e_%d", binary.BigEndian.Uint64(randomBytes[:]))

	if _, cerr := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", dbName)); cerr != nil {
		t.Fatalf("CREATE DATABASE %s: %v", dbName, cerr)
	}

	t.Cleanup(func() {
		cleanupCtx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		cleanupConn, cerr := pgx.Connect(cleanupCtx, adminURI)
		if cerr != nil {
			return
		}
		defer func() { _ = cleanupConn.Close(cleanupCtx) }()
		_, _ = cleanupConn.Exec(cleanupCtx,
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1 AND pid <> pg_backend_pid()",
			dbName)
		_, _ = cleanupConn.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	return fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
}

// stripARRegistryEnv returns a base env with AGENT_REGISTRY_* and
// SKIP_MIGRATIONS stripped so a developer-shell with these set doesn't
// leak into the test. PATH / HOME / GOPATH etc. are preserved.
func stripARRegistryEnv() []string {
	var out []string
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "AGENT_REGISTRY_") ||
			strings.HasPrefix(kv, "SKIP_MIGRATIONS=") {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// ============================================================================
// No-DB tier: these run on every CI push regardless of DB availability.
// ============================================================================

// TestE2E_MissingDSN — `arctl db migrate up` with no flag and no env
// exits non-zero and the error message points at the env var so an
// operator knows how to fix it.
func TestE2E_MissingDSN(t *testing.T) {
	r := runArctl(t, stripARRegistryEnv(), "db", "migrate", "up")
	if r.exitCode == 0 {
		t.Fatalf("expected non-zero exit; got 0\nstdout: %s\nstderr: %s", r.stdout, r.stderr)
	}
	if !strings.Contains(r.combined, "AGENT_REGISTRY_DATABASE_URL") {
		t.Errorf("error should name AGENT_REGISTRY_DATABASE_URL; got combined output:\n%s", r.combined)
	}
}

// TestE2E_DBUrlPrecedence — `--db-url` wins over
// AGENT_REGISTRY_DATABASE_URL. Both DSNs point at non-existent hosts
// with distinguishing user-tags; the connection error must reference
// the flag's user-tag, never the env's. Proves the precedence at the
// CLI's `withConn` resolution.
func TestE2E_DBUrlPrecedence(t *testing.T) {
	env := append(stripARRegistryEnv(),
		"AGENT_REGISTRY_DATABASE_URL=postgres://envtag@nonexistent.example.invalid:5432/db?sslmode=disable")
	r := runArctl(t, env,
		"db", "migrate", "status",
		"--db-url=postgres://flagtag@otherhost.example.invalid:5432/db?sslmode=disable",
	)
	if r.exitCode == 0 {
		t.Fatalf("expected non-zero exit (both DSNs invalid); got 0\nstdout: %s", r.stdout)
	}
	if !strings.Contains(r.combined, "flagtag") {
		t.Errorf("connection error should reference 'flagtag' (proving the flag won); got:\n%s", r.combined)
	}
	if strings.Contains(r.combined, "envtag") {
		t.Errorf("connection error should NOT reference 'envtag' (the env should have lost); got:\n%s", r.combined)
	}
}

// TestE2E_Help — `arctl db migrate --help` exits 0 and lists every
// documented subcommand. Catches accidental removal or registration
// regressions in cobra wiring.
func TestE2E_Help(t *testing.T) {
	r := runArctl(t, stripARRegistryEnv(), "db", "migrate", "--help")
	if r.exitCode != 0 {
		t.Fatalf("--help exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	for _, sub := range []string{"up", "down", "status", "version", "goto", "force"} {
		if !strings.Contains(r.stdout, sub) {
			t.Errorf("--help missing subcommand %q in stdout:\n%s", sub, r.stdout)
		}
	}
}

// TestE2E_ArgValidation — arg-validation failures exit non-zero with
// the expected message shape. The cobra unit tests cover these by
// calling RunE directly; this case exercises the same paths through
// the built binary so cobra wiring + error propagation regressions
// surface here too.
func TestE2E_ArgValidation(t *testing.T) {
	env := stripARRegistryEnv()
	cases := []struct {
		name      string
		args      []string
		wantInErr string
	}{
		{"down with non-integer", []string{"db", "migrate", "down", "abc"}, "positive integer"},
		{"down with zero", []string{"db", "migrate", "down", "0"}, "positive integer"},
		// goto's negative-int case is unreachable through the CLI —
		// cobra eats `-5` as an unknown shorthand flag before our
		// RunE arg validation runs. The non-integer case exercises
		// the same strconv.Atoi error path.
		{"goto with non-integer", []string{"db", "migrate", "goto", "abc"}, "non-negative integer"},
		{"force with zero", []string{"db", "migrate", "force", "0"}, "positive integer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := runArctl(t, env, tc.args...)
			if r.exitCode == 0 {
				t.Fatalf("expected non-zero exit; got 0\nstdout: %s", r.stdout)
			}
			if !strings.Contains(r.combined, tc.wantInErr) {
				t.Errorf("error should contain %q; got:\n%s", tc.wantInErr, r.combined)
			}
		})
	}
}

// ============================================================================
// DB-required tier: skipped automatically when Postgres is unavailable.
// ============================================================================

// TestE2E_HappyPath — `up` → `status` → `version` against a fresh
// Postgres. Asserts exit 0 on each step, the operator-facing stdout
// shape, and the post-Up version (8 — the highest OSS migration).
func TestE2E_HappyPath(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "up")
	if r.exitCode != 0 {
		t.Fatalf("up: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "schema is up to date") {
		t.Errorf("up stdout should contain 'schema is up to date'; got: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "status")
	if r.exitCode != 0 {
		t.Fatalf("status: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "7 migration(s) applied (at v8), 0 pending") {
		t.Errorf("status should show 7 applied (at v8), 0 pending; got: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "version")
	if r.exitCode != 0 {
		t.Fatalf("version: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	v := strings.TrimSpace(r.stdout)
	if v != "8" {
		t.Errorf("version should be 8 (highest OSS migration); got %q", v)
	}
}

// TestE2E_DownErrNotReversible — every OSS migration ships an
// up-only .down.sql that RAISES EXCEPTION. `down 1` after a
// successful `up` must fail and the error must mention
// "not reversible" so operators see why.
func TestE2E_DownErrNotReversible(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	if r := runArctl(t, env, "db", "migrate", "up"); r.exitCode != 0 {
		t.Fatalf("up setup: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}

	r := runArctl(t, env, "db", "migrate", "down", "1")
	if r.exitCode == 0 {
		t.Fatalf("down 1 against up-only OSS should fail; got exit 0\nstdout: %s", r.stdout)
	}
	if !strings.Contains(r.combined, "not reversible") {
		t.Errorf("error should reference 'not reversible'; got:\n%s", r.combined)
	}
}

// TestE2E_ForceWritesRowOnly — `force V` writes the schema_migrations
// row without running the migration SQL. After `force 1` against a
// fresh DB, status reports 1 applied and no public tables besides
// schema_migrations itself exist (force did NOT run 001's CREATE
// TABLE statements).
func TestE2E_ForceWritesRowOnly(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "force", "1")
	if r.exitCode != 0 {
		t.Fatalf("force 1: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "1") || !strings.Contains(r.stdout, "applied") {
		t.Errorf("force stdout shape unexpected: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "status")
	if r.exitCode != 0 {
		t.Fatalf("status: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "1 migration(s) applied") {
		t.Errorf("status should show 1 applied; got %q", r.stdout)
	}

	// Verify force did NOT run 001's CREATE SCHEMA / CREATE TABLE
	// statements. The only public table should be schema_migrations
	// (created by go-migrate's pgx/v5 driver on first use); no v1alpha1
	// schema should exist.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to verify: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var extraTables int
	if err := conn.QueryRow(ctx,
		"SELECT COUNT(*) FROM pg_tables WHERE schemaname = 'public' AND tablename != 'schema_migrations'",
	).Scan(&extraTables); err != nil {
		t.Fatalf("count public tables: %v", err)
	}
	if extraTables != 0 {
		t.Errorf("expected only schema_migrations in public schema after `force 1`; got %d extra tables", extraTables)
	}

	var v1alpha1Exists bool
	if err := conn.QueryRow(ctx,
		"SELECT EXISTS (SELECT 1 FROM information_schema.schemata WHERE schema_name = 'v1alpha1')",
	).Scan(&v1alpha1Exists); err != nil {
		t.Fatalf("probe v1alpha1 schema: %v", err)
	}
	if v1alpha1Exists {
		t.Errorf("v1alpha1 schema must NOT exist after `force 1` (migration SQL did not run)")
	}
}

// TestE2E_GotoInferredSource — single-source binaries accept
// per-source ops without --source. Asserts `goto 2` against a fresh
// DB advances the schema to version 2 and the version subcommand
// confirms it.
func TestE2E_GotoInferredSource(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "goto", "2")
	if r.exitCode != 0 {
		t.Fatalf("goto 2: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "schema is at version 2") {
		t.Errorf("goto 2 stdout unexpected: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "version")
	if r.exitCode != 0 {
		t.Fatalf("version: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "2" {
		t.Errorf("version should be 2; got %q", r.stdout)
	}
}

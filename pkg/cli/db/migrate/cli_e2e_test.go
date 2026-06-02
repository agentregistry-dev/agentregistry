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
	"encoding/json"
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

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
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
// shape, and the post-Up version (1 — the single collapsed migration).
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
	if !strings.Contains(r.stdout, "1 migration(s) applied (at v1), 0 pending") {
		t.Errorf("status should show 1 applied (at v1), 0 pending; got: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "version")
	if r.exitCode != 0 {
		t.Fatalf("version: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	v := strings.TrimSpace(r.stdout)
	if v != "1" {
		t.Errorf("version should be 1; got %q", v)
	}
}

// TestE2E_UpIsIdempotent — `up` followed by another `up` is a no-op;
// the second invocation reports "no pending migrations".
func TestE2E_UpIsIdempotent(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	if r := runArctl(t, env, "db", "migrate", "up"); r.exitCode != 0 {
		t.Fatalf("first up: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	r := runArctl(t, env, "db", "migrate", "up")
	if r.exitCode != 0 {
		t.Fatalf("second up: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "no pending migrations") {
		t.Errorf("second up should report 'no pending migrations'; got: %q", r.stdout)
	}
}

// TestE2E_DownErrNotReversible — 001_initial_schema ships an up-only
// .down.sql that RAISES EXCEPTION. `down 1` after a successful `up`
// must fail and the error must mention "not reversible" so operators
// see why.
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
// fresh DB, status reports 1 applied; the OSS schema's schema_migrations
// table exists but the data tables 001 would have created do not.
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

	// Verify force did NOT run 001's CREATE TABLE statements. The
	// only table in agentregistry should be schema_migrations
	// (created by migratepgx); no data tables.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect to verify: %v", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	var extraTables int
	if err := conn.QueryRow(ctx,
		"SELECT COUNT(*) FROM pg_tables WHERE schemaname = $1 AND tablename != 'schema_migrations'",
		database.OSSSchema,
	).Scan(&extraTables); err != nil {
		t.Fatalf("count %s tables: %v", database.OSSSchema, err)
	}
	if extraTables != 0 {
		t.Errorf("expected only schema_migrations in %s schema after `force 1`; got %d extra tables", database.OSSSchema, extraTables)
	}
}

// TestE2E_StatusJSON — `status --output json` emits a stable JSON
// shape that CI/CD shell scripts can pipe through `jq`.
func TestE2E_StatusJSON(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	if r := runArctl(t, env, "db", "migrate", "up"); r.exitCode != 0 {
		t.Fatalf("up setup: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}

	r := runArctl(t, env, "db", "migrate", "status", "--output", "json")
	if r.exitCode != 0 {
		t.Fatalf("status -o json: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}

	var payload struct {
		Applied int `json:"applied"`
		Pending int `json:"pending"`
		Sources []struct {
			Name       string `json:"name"`
			Applied    int    `json:"applied"`
			Pending    int    `json:"pending"`
			Version    int    `json:"version"`
			Downgraded bool   `json:"downgraded"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &payload); err != nil {
		t.Fatalf("status -o json output not parseable: %v\nstdout: %s", err, r.stdout)
	}
	if payload.Applied != 1 || payload.Pending != 0 {
		t.Errorf("aggregate counts: got applied=%d pending=%d; want 1/0", payload.Applied, payload.Pending)
	}
	if len(payload.Sources) != 1 {
		t.Fatalf("expected 1 source in JSON; got %d", len(payload.Sources))
	}
	s := payload.Sources[0]
	if s.Name != "oss" || s.Applied != 1 || s.Pending != 0 || s.Version != 1 || s.Downgraded {
		t.Errorf("source[0]: got %+v; want {oss 1 0 1 false}", s)
	}

	// Invalid --output rejected with a useful message.
	r = runArctl(t, env, "db", "migrate", "status", "--output", "yaml")
	if r.exitCode == 0 {
		t.Fatalf("status --output yaml should fail; got exit 0\nstdout: %s", r.stdout)
	}
	if !strings.Contains(r.combined, "supported: text, json") {
		t.Errorf("invalid --output error should name supported formats; got:\n%s", r.combined)
	}
}

// TestE2E_GotoInferredSource — single-source binaries accept
// per-source ops without --source. Asserts `goto 1` against a fresh
// DB advances the schema to version 1 (forward) and version confirms it.
func TestE2E_GotoInferredSource(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "goto", "1")
	if r.exitCode != 0 {
		t.Fatalf("goto 1: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "schema is at version 1") {
		t.Errorf("goto 1 stdout unexpected: %q", r.stdout)
	}

	r = runArctl(t, env, "db", "migrate", "version")
	if r.exitCode != 0 {
		t.Fatalf("version: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if strings.TrimSpace(r.stdout) != "1" {
		t.Errorf("version should be 1; got %q", r.stdout)
	}
}

// TestE2E_ForceVersionOutOfRange — `force V` for V not in the shipped
// migration set must fail with a message naming the valid versions,
// preventing operators from wedging the DB by typoing a high integer.
func TestE2E_ForceVersionOutOfRange(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "force", "9999")
	if r.exitCode == 0 {
		t.Fatalf("force 9999 should fail; got exit 0\nstdout: %s", r.stdout)
	}
	if !strings.Contains(r.combined, "not a shipped migration") {
		t.Errorf("error should reference 'not a shipped migration'; got:\n%s", r.combined)
	}
	if !strings.Contains(r.combined, "valid versions") {
		t.Errorf("error should list valid versions; got:\n%s", r.combined)
	}
}

// TestE2E_VersionFreshDBDisambiguated — `version` on an unbridged
// fresh DB must distinguish "no migrations applied" from a binary error.
func TestE2E_VersionFreshDBDisambiguated(t *testing.T) {
	dsn := freshDB(t)
	env := append(stripARRegistryEnv(), "AGENT_REGISTRY_DATABASE_URL="+dsn)

	r := runArctl(t, env, "db", "migrate", "version")
	if r.exitCode != 0 {
		t.Fatalf("version on fresh DB: exit %d\nstderr: %s", r.exitCode, r.stderr)
	}
	if !strings.Contains(r.stdout, "no migrations applied") {
		t.Errorf("version output should disambiguate fresh DB; got: %q", r.stdout)
	}
}

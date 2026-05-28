package v1alpha1store_test

import (
	"bufio"
	"bytes"
	"io/fs"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/v1alpha1store"
)

// TestMigrationsLint walks the embedded migration set and rejects
// patterns that pin migrations to a specific Postgres schema. The
// runtime schema is set via migratepgx.Config{SchemaName: ...} and
// resolved through search_path, so every identifier in a migration
// must stay unqualified.
//
// Failing patterns surface as `filename:line — match` so authors land
// straight on the offending line.
func TestMigrationsLint(t *testing.T) {
	violations := lintMigrations(t, v1alpha1store.MigrationFiles, v1alpha1store.MigrationsDir)
	for _, v := range violations {
		t.Errorf("%s", v)
	}
}

// lintMigrations is factored out so the negative test below can
// reach in and exercise it with a fixture FS without touching the
// real embed.
func lintMigrations(t *testing.T, files fs.FS, dir string) []string {
	t.Helper()
	var violations []string
	err := fs.WalkDir(files, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".sql") {
			return nil
		}
		data, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		violations = append(violations, scanSQL(path, data)...)
		return nil
	})
	if err != nil {
		t.Fatalf("walk migrations FS: %v", err)
	}
	return violations
}

// forbiddenPatterns is the catalogue of rejected substrings. Each
// entry pairs a regex with a human-readable explanation surfaced when
// a migration trips the lint.
var forbiddenPatterns = []struct {
	re      *regexp.Regexp
	message string
}{
	{
		re:      regexp.MustCompile(`\b(?:v1alpha1|public|agentregistry|pg_catalog|information_schema)\.`),
		message: "schema-qualified identifier (drop the prefix; the driver sets search_path)",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE\s+SCHEMA\b`),
		message: "CREATE SCHEMA is not allowed in migrations (the orchestrator creates the schema before applying the migration)",
	},
	{
		re:      regexp.MustCompile(`(?i)\bSET\s+search_path\b`),
		message: "SET search_path is not allowed in migrations (the driver sets it)",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+\w+\.\w+\b`),
		message: "CREATE TABLE in a non-default schema is not allowed (the CREATE TABLE x.y rule catches arbitrary downstream schemas without naming them)",
	},
}

// scanSQL applies the forbidden-pattern catalogue to a single file.
// Returns one violation string per match.
func scanSQL(path string, data []byte) []string {
	var out []string
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		// Strip SQL line comments so commentary doesn't flag the lint.
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		for _, p := range forbiddenPatterns {
			if loc := p.re.FindStringIndex(line); loc != nil {
				out = append(out, formatViolation(path, lineNum, line[loc[0]:loc[1]], p.message))
			}
		}
	}
	return out
}

func formatViolation(path string, line int, match, message string) string {
	return path + ":" + strconv.Itoa(line) + " — " + message + " (matched: " + match + ")"
}

// TestMigrationsLint_FlagsForbiddenPatterns asserts each category of
// forbidden pattern surfaces as a violation with a clear message.
// Catches regressions in the lint when the regex catalogue evolves.
func TestMigrationsLint_FlagsForbiddenPatterns(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"schema-qualified identifier",
			"CREATE TABLE IF NOT EXISTS v1alpha1.agents (id int);",
			"schema-qualified identifier",
		},
		{
			"CREATE SCHEMA",
			"CREATE SCHEMA agentregistry;",
			"CREATE SCHEMA is not allowed",
		},
		{
			"SET search_path",
			"SET search_path TO agentregistry;",
			"SET search_path is not allowed",
		},
		{
			"CREATE TABLE non-default schema",
			"CREATE TABLE other.foo (id int);",
			"CREATE TABLE in a non-default schema",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fixture := fstest.MapFS{
				"migrations/999_test.up.sql": &fstest.MapFile{Data: []byte(tc.body)},
			}
			violations := lintMigrations(t, fixture, "migrations")
			if len(violations) == 0 {
				t.Fatalf("expected at least one violation for body %q, got none", tc.body)
			}
			found := false
			for _, v := range violations {
				if strings.Contains(v, tc.want) {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("expected violation containing %q; got %v", tc.want, violations)
			}
		})
	}
}

// TestMigrationsLint_IgnoresComments confirms the lint strips SQL line
// comments before matching — commentary like "removes the v1alpha1.
// prefix" must not trip the lint.
func TestMigrationsLint_IgnoresComments(t *testing.T) {
	fixture := fstest.MapFS{
		"migrations/999_test.up.sql": &fstest.MapFile{
			Data: []byte("-- this migration drops the v1alpha1. prefix\nCREATE TABLE agents (id int);\n"),
		},
	}
	violations := lintMigrations(t, fixture, "migrations")
	if len(violations) != 0 {
		t.Errorf("expected no violations on commentary-only mention; got %v", violations)
	}
}

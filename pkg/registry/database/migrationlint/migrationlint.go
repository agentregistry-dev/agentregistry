// Package migrationlint enforces the SQL conventions every migration
// source must follow, so the OSS set and any downstream set are held to
// the same contract. Two rule families:
//
//   - Schema-agnostic: no schema-qualified identifiers, no CREATE SCHEMA,
//     no SET search_path. The runtime schema is set via the migrator's
//     SchemaName/search_path, so migration SQL must stay unqualified.
//   - Single-transaction atomicity: no CONCURRENTLY, no explicit
//     transaction control, no other statement Postgres refuses to run
//     inside a transaction block. The migrator applies each file as one
//     implicit transaction; the orchestrator's failure-restore relies on
//     a failed file rolling back its DDL rather than half-applying.
//
// Each migration set runs Check against its own embedded FS from a test:
//
//	func TestMigrationsLint(t *testing.T) {
//	    violations, err := migrationlint.Check(myMigrations, "migrations")
//	    if err != nil {
//	        t.Fatal(err)
//	    }
//	    for _, v := range violations {
//	        t.Errorf("%s", v)
//	    }
//	}
//
// The rules mirror the contract documented in the OSS migrations README.
package migrationlint

import (
	"bufio"
	"bytes"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
)

// Violation is a single rejected line in a migration file.
type Violation struct {
	// File is the path within the linted FS.
	File string
	// Line is the 1-based line number of the match.
	Line int
	// Match is the substring that tripped the rule.
	Match string
	// Message explains why the pattern is rejected.
	Message string
}

// String renders a violation as `file:line — message (matched: x)` so a
// test failure points the author straight at the offending line.
func (v Violation) String() string {
	return fmt.Sprintf("%s:%d — %s (matched: %s)", v.File, v.Line, v.Message, v.Match)
}

// Check walks every `*.sql` file under dir in files and returns one
// Violation per rejected line. A nil/empty slice means the set is clean.
// The returned error is non-nil only on an FS walk/read failure, not on
// lint violations.
func Check(files fs.FS, dir string) ([]Violation, error) {
	var violations []Violation
	err := fs.WalkDir(files, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".sql") {
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
		return nil, fmt.Errorf("walk migrations FS: %w", err)
	}
	return violations, nil
}

// qualifiedIdent matches a schema-qualified identifier in either bare
// or double-quoted form: word.word, "word".word, word."word", or
// "word"."word". Spaces around the dot are tolerated (Postgres accepts
// them).
const qualifiedIdent = `(?:\w+|"[^"]+")\s*\.\s*(?:\w+|"[^"]+")`

// forbiddenPatterns is the catalogue of rejected substrings. Each
// entry pairs a regex with a human-readable explanation surfaced when
// a migration trips the lint.
var forbiddenPatterns = []struct {
	re      *regexp.Regexp
	message string
}{
	{
		// Match either bare `v1alpha1.` or quoted `"v1alpha1".` and
		// the same for the other named schemas.
		re:      regexp.MustCompile(`(?:\b(?:v1alpha1|public|agentregistry|pg_catalog|information_schema)\b|"(?:v1alpha1|public|agentregistry|pg_catalog|information_schema)")\s*\.`),
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
		re:      regexp.MustCompile(`(?i)\bCREATE\s+TABLE(?:\s+IF\s+NOT\s+EXISTS)?\s+` + qualifiedIdent),
		message: "CREATE TABLE in a non-default schema is not allowed (the CREATE TABLE x.y rule catches arbitrary downstream schemas without naming them)",
	},
	{
		re:      regexp.MustCompile(`(?i)\bALTER\s+TABLE(?:\s+IF\s+EXISTS)?\s+(?:ONLY\s+)?` + qualifiedIdent),
		message: "ALTER TABLE addressing a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE(?:\s+UNIQUE)?\s+INDEX(?:\s+CONCURRENTLY)?(?:\s+IF\s+NOT\s+EXISTS)?\s+(?:\w+|"[^"]+")\s+ON\s+` + qualifiedIdent),
		message: "CREATE INDEX targeting a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE(?:\s+OR\s+REPLACE)?\s+VIEW(?:\s+IF\s+NOT\s+EXISTS)?\s+` + qualifiedIdent),
		message: "CREATE VIEW in a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE\s+SEQUENCE(?:\s+IF\s+NOT\s+EXISTS)?\s+` + qualifiedIdent),
		message: "CREATE SEQUENCE in a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE\s+TYPE\s+` + qualifiedIdent),
		message: "CREATE TYPE in a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bALTER\s+(?:INDEX|SEQUENCE|TYPE|VIEW|FUNCTION)(?:\s+IF\s+EXISTS)?\s+` + qualifiedIdent),
		message: "ALTER addressing a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCOMMENT\s+ON\s+\w+\s+` + qualifiedIdent),
		message: "COMMENT ON targeting a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\b(?:GRANT|REVOKE)\b.*?\bON\s+` + qualifiedIdent),
		message: "GRANT/REVOKE targeting a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE(?:\s+OR\s+REPLACE)?\s+TRIGGER\s+(?:\w+|"[^"]+")\s+(?:BEFORE|AFTER|INSTEAD\s+OF)\b.*?\bON\s+` + qualifiedIdent),
		message: "CREATE TRIGGER targeting a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bCREATE(?:\s+OR\s+REPLACE)?\s+FUNCTION\s+` + qualifiedIdent + `\s*\(`),
		message: "CREATE FUNCTION in a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + qualifiedIdent),
		message: "INSERT INTO addressing a non-default schema is not allowed",
	},
	{
		re:      regexp.MustCompile(`(?i)\bDROP\s+(?:TABLE|INDEX|TRIGGER|FUNCTION|VIEW|MATERIALIZED\s+VIEW|SEQUENCE|TYPE)(?:\s+IF\s+EXISTS)?\s+` + qualifiedIdent),
		message: "DROP addressing a non-default schema is not allowed",
	},
	{
		// Inline foreign-key target — `REFERENCES <schema>.<table>` in a
		// column or table constraint. No leading statement keyword
		// introduces these the way CREATE/INSERT/DROP do, so a qualified
		// FK target has no dedicated rule above and would otherwise slip
		// through. Matches any schema via qualifiedIdent, so it needs no
		// schema-name list. An unqualified `REFERENCES bar(id)` has no dot
		// and does not match.
		re:      regexp.MustCompile(`(?i)\bREFERENCES\s+` + qualifiedIdent),
		message: "REFERENCES targeting a non-default schema is not allowed",
	},
	{
		// Qualified table in a FROM/JOIN — a trigger/function body or a
		// backfill SELECT reaching into another schema. Like REFERENCES,
		// this is a context no DDL rule above covers. `FROM <table>` /
		// `JOIN <table>` unqualified has no dot and does not match.
		re:      regexp.MustCompile(`(?i)\b(?:FROM|JOIN)\s+` + qualifiedIdent),
		message: "FROM/JOIN targeting a non-default schema is not allowed",
	},
	{
		// `UPDATE <schema>.<table>` — a backfill or in-place data fix
		// reaching into another schema. Its own keyword, no rule above.
		// `UPDATE <table>` unqualified has no dot and does not match.
		re:      regexp.MustCompile(`(?i)\bUPDATE\s+` + qualifiedIdent),
		message: "UPDATE targeting a non-default schema is not allowed",
	},
	{
		// `TRUNCATE [TABLE] <schema>.<table>`. TRUNCATE accepts an
		// optional TABLE keyword before the target, so allow it between.
		// `TRUNCATE <table>` unqualified has no dot and does not match.
		re:      regexp.MustCompile(`(?i)\bTRUNCATE\s+(?:TABLE\s+)?` + qualifiedIdent),
		message: "TRUNCATE targeting a non-default schema is not allowed",
	},
	{
		// CONCURRENTLY (CREATE/DROP INDEX, REINDEX) cannot run inside a
		// transaction block. The driver wraps each migration file in one
		// implicit transaction; a CONCURRENTLY statement would either error
		// or, on its own, apply non-atomically — breaking the
		// single-transaction invariant the orchestrator's failure-restore
		// relies on (a failed file must roll back its DDL, leaving only a
		// dirty marker, never half-applied schema).
		re:      regexp.MustCompile(`(?i)\bCONCURRENTLY\b`),
		message: "CONCURRENTLY is not allowed (it breaks the single-transaction atomicity the orchestrator's failure-restore depends on)",
	},
	{
		// Explicit COMMIT/ROLLBACK ends the implicit transaction early, so
		// statements after it apply non-atomically. Anchored to statement
		// forms (`COMMIT;`, `COMMIT WORK`, end-of-line) so the words inside
		// string literals or identifiers like `commit_sha` don't trip it.
		re:      regexp.MustCompile(`(?i)\b(?:COMMIT|ROLLBACK)\b\s*(?:;|WORK\b|TRANSACTION\b|AND\b|$)`),
		message: "explicit COMMIT/ROLLBACK is not allowed (it breaks the single-transaction atomicity the orchestrator's failure-restore depends on)",
	},
	{
		// Explicit transaction openers. PL/pgSQL block `BEGIN` (as in
		// `DO $$ BEGIN ... END $$`) is never written `BEGIN;` / `BEGIN
		// TRANSACTION` / `BEGIN WORK`, so only the transaction-control forms
		// match.
		re:      regexp.MustCompile(`(?i)(?:\bSTART\s+TRANSACTION\b|\bBEGIN\s+TRANSACTION\b|\bBEGIN\s+WORK\b|\bBEGIN\s*;)`),
		message: "explicit transaction control (BEGIN/START TRANSACTION) is not allowed (the driver wraps each migration file in one implicit transaction)",
	},
	{
		// Statements Postgres refuses to run inside a transaction block.
		// In a multi-statement file they fail loudly ("cannot run inside a
		// transaction block"); alone they apply non-atomically. Either way
		// they break the single-transaction guarantee the orchestrator's
		// failure-restore relies on, so reject them at lint time rather
		// than apply time. This is not exhaustive — any other
		// non-transactional statement is equally unsafe — but it catches
		// the ones most likely to be written.
		re:      regexp.MustCompile(`(?i)(?:\bVACUUM\b|\b(?:CREATE|DROP)\s+(?:DATABASE|TABLESPACE)\b|\bALTER\s+SYSTEM\b)`),
		message: "non-transactional statement (VACUUM / CREATE|DROP DATABASE|TABLESPACE / ALTER SYSTEM) is not allowed (it cannot run inside the single transaction the driver wraps each migration file in)",
	},
}

// scanSQL applies the forbidden-pattern catalogue to a single file,
// returning one Violation per match.
func scanSQL(path string, data []byte) []Violation {
	var out []Violation
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		// Strip SQL line comments so commentary doesn't flag the lint.
		// String literals are NOT stripped, so a forbidden keyword inside
		// a string (e.g. 'ALTER SYSTEM disabled') would false-positive.
		// Accepted: migrations don't carry such literals, and a false
		// positive fails safe (rejects a migration that would have applied).
		if idx := strings.Index(line, "--"); idx >= 0 {
			line = line[:idx]
		}
		for _, p := range forbiddenPatterns {
			if loc := p.re.FindStringIndex(line); loc != nil {
				out = append(out, Violation{
					File:    path,
					Line:    lineNum,
					Match:   line[loc[0]:loc[1]],
					Message: p.message,
				})
			}
		}
	}
	return out
}

package migrationlint_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database/migrationlint"
)

// checkBody runs Check over a single-file fixture and returns the
// violation strings.
func checkBody(t *testing.T, body string) []string {
	t.Helper()
	fixture := fstest.MapFS{
		"migrations/999_test.up.sql": &fstest.MapFile{Data: []byte(body)},
	}
	violations, err := migrationlint.Check(fixture, "migrations")
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	out := make([]string, len(violations))
	for i, v := range violations {
		out[i] = v.String()
	}
	return out
}

// TestCheck_FlagsForbiddenPatterns asserts each category of forbidden
// pattern surfaces as a violation with a clear message. Catches
// regressions in the lint when the regex catalogue evolves.
func TestCheck_FlagsForbiddenPatterns(t *testing.T) {
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
		{
			"ALTER TABLE non-default schema",
			"ALTER TABLE other.foo ADD COLUMN bar int;",
			"ALTER TABLE addressing a non-default schema",
		},
		{
			"CREATE INDEX non-default schema",
			"CREATE INDEX foo_idx ON other.foo (id);",
			"CREATE INDEX targeting a non-default schema",
		},
		{
			"CREATE TRIGGER non-default schema",
			"CREATE TRIGGER foo_trg BEFORE UPDATE ON other.foo FOR EACH ROW EXECUTE FUNCTION noop();",
			"CREATE TRIGGER targeting a non-default schema",
		},
		{
			"CREATE FUNCTION non-default schema",
			"CREATE OR REPLACE FUNCTION other.fn() RETURNS void AS $$ BEGIN END; $$;",
			"CREATE FUNCTION in a non-default schema",
		},
		{
			"INSERT INTO non-default schema",
			"INSERT INTO other.foo (id) VALUES (1);",
			"INSERT INTO addressing a non-default schema",
		},
		{
			"DROP non-default schema",
			"DROP TABLE IF EXISTS other.foo;",
			"DROP addressing a non-default schema",
		},
		{
			"DROP VIEW non-default schema",
			"DROP VIEW other.v;",
			"DROP addressing a non-default schema",
		},
		{
			"DROP MATERIALIZED VIEW non-default schema",
			"DROP MATERIALIZED VIEW IF EXISTS other.mv;",
			"DROP addressing a non-default schema",
		},
		{
			"DROP SEQUENCE non-default schema",
			"DROP SEQUENCE other.seq;",
			"DROP addressing a non-default schema",
		},
		{
			"DROP TYPE non-default schema",
			"DROP TYPE other.t;",
			"DROP addressing a non-default schema",
		},
		{
			// Inline FK into another schema — the context that has no
			// leading-keyword rule of its own.
			"REFERENCES non-default schema",
			"CREATE TABLE foo (bar_id int REFERENCES other.bar(id));",
			"REFERENCES targeting a non-default schema",
		},
		{
			"REFERENCES quoted identifiers",
			`CREATE TABLE foo (bar_id int REFERENCES "other"."bar"(id));`,
			"REFERENCES targeting a non-default schema",
		},
		{
			// Qualified table reached through FROM, not a DDL target.
			"FROM non-default schema",
			"INSERT INTO foo SELECT id FROM other.bar;",
			"FROM/JOIN targeting a non-default schema",
		},
		{
			"JOIN non-default schema",
			"CREATE VIEW v AS SELECT a.id FROM a JOIN other.bar b ON b.id = a.id;",
			"FROM/JOIN targeting a non-default schema",
		},
		{
			"UPDATE non-default schema",
			"UPDATE other.foo SET x = 1;",
			"UPDATE targeting a non-default schema",
		},
		{
			"TRUNCATE non-default schema",
			"TRUNCATE other.foo;",
			"TRUNCATE targeting a non-default schema",
		},
		{
			"TRUNCATE TABLE non-default schema",
			"TRUNCATE TABLE other.foo;",
			"TRUNCATE targeting a non-default schema",
		},
		{
			"CREATE INDEX CONCURRENTLY non-default schema",
			"CREATE INDEX CONCURRENTLY foo_idx ON other.foo (id);",
			"CREATE INDEX targeting a non-default schema",
		},
		{
			"CREATE VIEW non-default schema",
			"CREATE VIEW other.foo AS SELECT 1;",
			"CREATE VIEW in a non-default schema",
		},
		{
			"CREATE SEQUENCE non-default schema",
			"CREATE SEQUENCE other.foo_seq;",
			"CREATE SEQUENCE in a non-default schema",
		},
		{
			"CREATE TYPE non-default schema",
			"CREATE TYPE other.foo_t AS ENUM ('a', 'b');",
			"CREATE TYPE in a non-default schema",
		},
		{
			"ALTER INDEX non-default schema",
			"ALTER INDEX other.foo_idx RENAME TO bar_idx;",
			"ALTER addressing a non-default schema",
		},
		{
			"COMMENT ON non-default schema",
			"COMMENT ON TABLE other.foo IS 'a foo';",
			"COMMENT ON targeting a non-default schema",
		},
		{
			"GRANT non-default schema",
			"GRANT SELECT ON other.foo TO public;",
			"GRANT/REVOKE targeting a non-default schema",
		},
		{
			"CREATE TABLE quoted identifiers",
			`CREATE TABLE "other"."foo" (id int);`,
			"CREATE TABLE in a non-default schema",
		},
		{
			"INSERT INTO quoted identifiers",
			`INSERT INTO "other"."foo" (id) VALUES (1);`,
			"INSERT INTO addressing a non-default schema",
		},
		{
			"CREATE INDEX CONCURRENTLY (unqualified)",
			"CREATE INDEX CONCURRENTLY foo_idx ON foo (id);",
			"CONCURRENTLY is not allowed",
		},
		{
			"explicit COMMIT",
			"CREATE TABLE foo (id int);\nCOMMIT;",
			"explicit COMMIT/ROLLBACK is not allowed",
		},
		{
			"explicit BEGIN;",
			"BEGIN;\nCREATE TABLE foo (id int);",
			"explicit transaction control",
		},
		{
			"explicit START TRANSACTION",
			"START TRANSACTION;\nCREATE TABLE foo (id int);",
			"explicit transaction control",
		},
		{
			"VACUUM",
			"VACUUM ANALYZE foo;",
			"non-transactional statement",
		},
		{
			"CREATE DATABASE",
			"CREATE DATABASE foo;",
			"non-transactional statement",
		},
		{
			"ALTER SYSTEM",
			"ALTER SYSTEM SET work_mem = '64MB';",
			"non-transactional statement",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			violations := checkBody(t, tc.body)
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

// TestCheck_AllowsPlpgsqlBlocks confirms the transaction-control rules
// don't false-positive on PL/pgSQL block syntax — `DO $$ BEGIN ... END
// $$`, `END IF;`, and a trigger function body — none of which are
// transaction control. The real OSS `001` down file uses the DO/BEGIN/END
// form.
func TestCheck_AllowsPlpgsqlBlocks(t *testing.T) {
	bodies := []string{
		"DO $$ BEGIN\n  RAISE EXCEPTION 'not reversible';\nEND $$;",
		"CREATE OR REPLACE FUNCTION notify_fn() RETURNS trigger AS $$\nBEGIN\n  PERFORM pg_notify('agents_status', NEW.id::text);\n  RETURN NEW;\nEND;\n$$ LANGUAGE plpgsql;",
		"DO $$ BEGIN\n  IF NOT EXISTS (SELECT 1) THEN\n    NULL;\n  END IF;\nEND $$;",
		"INSERT INTO audit (action) VALUES ('commit');",
		"CREATE TABLE foo (commit_sha text);",
		// Unqualified FK/FROM/UPDATE/TRUNCATE must stay clean — these rules
		// only fire on a schema-qualified target.
		"CREATE TABLE foo (bar_id int REFERENCES bar(id));",
		"INSERT INTO foo SELECT id FROM bar;",
		"CREATE VIEW v AS SELECT a.id FROM a JOIN b ON b.id = a.id;",
		"UPDATE foo SET x = 1;",
		"TRUNCATE TABLE foo;",
	}
	for _, body := range bodies {
		if violations := checkBody(t, body); len(violations) != 0 {
			t.Errorf("expected no violations for PL/pgSQL body %q; got %v", body, violations)
		}
	}
}

// TestCheck_IgnoresComments confirms the lint strips SQL line comments
// before matching — commentary like "removes the v1alpha1. prefix" must
// not trip the lint.
func TestCheck_IgnoresComments(t *testing.T) {
	body := "-- this migration drops the v1alpha1. prefix\nCREATE TABLE agents (id int);\n"
	if violations := checkBody(t, body); len(violations) != 0 {
		t.Errorf("expected no violations on commentary-only mention; got %v", violations)
	}
}

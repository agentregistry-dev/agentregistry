package database

import (
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// OSSSourceName is the registry key the OSS migration source registers
// under (orchestrator.Source.Name). Defined here so the migration source
// (legacymigrate) and the store layer (v1alpha1store) agree on the key
// without importing each other.
const OSSSourceName = "oss"

// schemaNameRE constrains a schema name to a lowercase, unquoted-safe
// Postgres identifier. Lowercase-only is deliberate: the schema name is
// used both quoted (CREATE SCHEMA, qualified queries) and unquoted (the
// search_path startup parameter, the to_regclass existence probes), and
// Postgres case-folds unquoted identifiers. Allowing mixed case would
// make those two forms refer to different schemas (`"MySchema"` vs the
// folded `myschema`), so the existence probe would never match the
// created schema. Restricting to lowercase keeps the two forms identical
// under case-folding. (It does not reconcile a lowercase *reserved word*
// like `user`, which parses as a keyword unquoted but an identifier
// quoted — avoiding reserved words is the schema author's responsibility,
// and matters once the schema name becomes operator input. Quoted() still
// quotes the name, so this is also fail-fast validation against
// typos/garbage, not the injection barrier itself.)
var schemaNameRE = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// Schema is a validated Postgres schema name with its quoted identifier
// precomputed once. Pass a Schema where schema-qualified SQL or a
// search_path is built, so the raw-vs-quoted distinction lives in one
// place and Sanitize runs at construction rather than per query.
//
// Use Name for the raw value (search_path DSN startup parameter,
// to_regclass text argument, log fields) and Quoted / Qualify wherever
// the schema is interpolated into a SQL statement.
type Schema struct {
	name   string
	quoted string
}

// NewSchema validates name as a plain identifier and precomputes its
// quoted form. Returns an error on an empty or non-identifier name.
func NewSchema(name string) (Schema, error) {
	if !schemaNameRE.MatchString(name) {
		return Schema{}, fmt.Errorf("invalid schema name %q: must match %s", name, schemaNameRE.String())
	}
	return Schema{name: name, quoted: pgx.Identifier{name}.Sanitize()}, nil
}

// MustNewSchema is NewSchema for a compile-time-known schema name (a
// const or an init-time registration value). It panics on an invalid
// name, surfacing the misconfiguration at startup rather than threading
// an error through a constructor. Use NewSchema where the name is
// runtime/operator input.
func MustNewSchema(name string) Schema {
	s, err := NewSchema(name)
	if err != nil {
		panic(err)
	}
	return s
}

// Name is the raw schema name. Use for the search_path connection
// parameter (libpq passes the literal value, unquoted), to_regclass text
// arguments, and log fields.
func (s Schema) Name() string { return s.name }

// Quoted is the schema as a double-quoted SQL identifier. Use when
// interpolating the schema alone into SQL: `CREATE SCHEMA <Quoted>`,
// `SET search_path TO <Quoted>`.
func (s Schema) Quoted() string { return s.quoted }

// Qualify returns `<Quoted>.<sanitized table>` — a schema-qualified,
// injection-safe table reference for SQL. Use for any query that must
// name the schema explicitly rather than rely on search_path (e.g. a
// cross-schema read, or a store that may run on a connection whose
// search_path points elsewhere).
func (s Schema) Qualify(table string) string {
	return s.quoted + "." + pgx.Identifier{table}.Sanitize()
}

// SchemaRegistry resolves a migration source's name to its Schema. It is
// the single source of truth for the schemas a process knows about:
// built once at the composition root from the registered sources and
// injected where schema-qualified SQL is built. Not safe for concurrent
// Add; populate it fully during startup before serving.
type SchemaRegistry struct {
	byName map[string]Schema
}

// NewSchemaRegistry returns an empty registry.
func NewSchemaRegistry() *SchemaRegistry {
	return &SchemaRegistry{byName: make(map[string]Schema)}
}

// Add validates schemaName, precomputes its quoted form, and registers it
// under source. Errors on an invalid schema name or a duplicate source.
func (r *SchemaRegistry) Add(source, schemaName string) error {
	if _, ok := r.byName[source]; ok {
		return fmt.Errorf("schema for source %q already registered", source)
	}
	sch, err := NewSchema(schemaName)
	if err != nil {
		return fmt.Errorf("register schema for source %q: %w", source, err)
	}
	r.byName[source] = sch
	return nil
}

// Get returns the Schema registered for source.
func (r *SchemaRegistry) Get(source string) (Schema, bool) {
	sch, ok := r.byName[source]
	return sch, ok
}

// OSSSchemaRegistry returns a registry preloaded with the OSS source's
// schema (OSSSourceName → OSSSchema). The composition root injects it
// into the OSS stores; a multi-source build adds its own sources via Add
// before injecting. OSSSchema is a compile-time-valid identifier, so the
// Add cannot fail.
func OSSSchemaRegistry() *SchemaRegistry {
	r := NewSchemaRegistry()
	_ = r.Add(OSSSourceName, OSSSchema)
	return r
}

// MustGet returns the Schema for source, panicking if absent. Use only
// where the source is statically known to be registered (e.g. the OSS
// store layer resolving OSSSourceName at construction); the panic
// surfaces a wiring bug at startup rather than a nil deref later.
func (r *SchemaRegistry) MustGet(source string) Schema {
	sch, ok := r.byName[source]
	if !ok {
		panic(fmt.Sprintf("database: no schema registered for source %q", source))
	}
	return sch
}

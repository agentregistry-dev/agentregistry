package database_test

import (
	"context"
	"embed"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

//go:embed testdata/down_fixture/*.sql
var exampleMigrationFiles embed.FS

// ExampleMigrator illustrates the API a downstream binary (e.g. an
// enterprise extension) uses to share schema_migrations with the OSS
// migrator. The OSS source sets VersionOffset=0 + EnsureTable=true to
// create the table; this downstream source sets a higher VersionOffset
// (typically 500+) + EnsureTable=false to coexist without colliding on
// version numbers. The example does not run — there is no live pgx
// connection — but it compiles and serves as the contract reference
// for the public Migrator surface.
func ExampleMigrator() {
	// REPLACE THIS with a live connection before running. Calling
	// NewMigrator and then any method below against a nil conn panics
	// — the example does not declare an `// Output:` directive, so go
	// test compiles it but never executes it. Production callers must
	// open a connection first:
	//
	//   conn, err := pgx.Connect(ctx, "postgres://...")
	//   if err != nil { ... }
	//   defer conn.Close(ctx)
	var conn *pgx.Conn

	cfg := database.MigratorConfig{
		MigrationFiles: exampleMigrationFiles,
		MigrationDir:   "testdata/down_fixture",
		VersionOffset:  500, // downstream extension range starts here
		EnsureTable:    false,
	}
	m := database.NewMigrator(conn, cfg)

	ctx := context.Background()

	// Apply all pending migrations forward.
	if err := m.Migrate(ctx); err != nil {
		fmt.Println("migrate:", err)
		return
	}

	// Inspect state: applied versions in this source's range and any
	// pending migrations from the file set that aren't applied yet.
	applied, pending, err := m.Status(ctx)
	if err != nil {
		fmt.Println("status:", err)
		return
	}
	fmt.Printf("applied=%v pending=%d\n", applied, len(pending))

	// Roll back the most-recent migration; errors with ErrNotReversible
	// if it has no .down.sql sibling.
	if err := m.Down(ctx, 1); err != nil {
		if errors.Is(err, database.ErrNotReversible) {
			fmt.Println("not reversible:", err)
			return
		}
		fmt.Println("down:", err)
		return
	}

	// Force-mark a specific version as applied (reconciliation after
	// manual remediation); errors with ErrOutOfRange when version is
	// outside this source's VersionOffset range.
	if err := m.Force(ctx, 502); err != nil {
		if errors.Is(err, database.ErrOutOfRange) {
			fmt.Println("out of range:", err)
		}
	}
}

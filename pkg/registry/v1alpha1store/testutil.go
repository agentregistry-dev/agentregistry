//go:build integration

package v1alpha1store

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// NewTestPool spins up a fresh database with the v1alpha1 schema
// applied and returns a connection pool scoped to it. Each test gets
// its own DB, cleaned up on t.Cleanup.
//
// Uses a `agent_registry_v1alpha1_template` template DB to amortize
// migration cost across tests. Requires PostgreSQL on localhost:5432;
// tests skip when it's unavailable.
func NewTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	adminURI := "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	if err := ensureTemplate(ctx, adminConn); err != nil {
		t.Fatalf("ensure v1alpha1 template: %v", err)
	}

	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err)
	dbName := fmt.Sprintf("test_v1alpha1_%d", binary.BigEndian.Uint64(randomBytes[:]))

	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, v1alpha1TemplateDBName))
	require.NoError(t, err)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		adminCleanup, err := pgx.Connect(cleanupCtx, adminURI)
		if err != nil {
			return
		}
		defer func() { _ = adminCleanup.Close(cleanupCtx) }()
		_, _ = adminCleanup.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()",
			dbName,
		))
		_, _ = adminCleanup.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	testURI := fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)
	pool, err := pgxpool.New(ctx, testURI)
	require.NoError(t, err)
	t.Cleanup(func() { pool.Close() })
	return pool
}

const v1alpha1TemplateDBName = "agent_registry_v1alpha1_template"

// ensureTemplate creates (idempotently) a template database with the
// v1alpha1 migrations applied. Uses pg_advisory_lock to serialize concurrent
// test processes.
func ensureTemplate(ctx context.Context, adminConn *pgx.Conn) error {
	const lockKey int64 = 0x76316131 // "v1a1"
	if _, err := adminConn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = adminConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	var exists bool
	if err := adminConn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)",
		v1alpha1TemplateDBName).Scan(&exists); err != nil {
		return fmt.Errorf("check template: %w", err)
	}

	if !exists {
		if _, err := adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", v1alpha1TemplateDBName)); err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && (pgErr.Code == "42P04" ||
				(pgErr.Code == "23505" && pgErr.ConstraintName == "pg_database_datname_index")) {
				// Concurrent creator won the race — fine.
			} else {
				return fmt.Errorf("create template: %w", err)
			}
		}
	}

	templateURI := fmt.Sprintf(
		"postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable",
		v1alpha1TemplateDBName)
	templateConn, err := pgx.Connect(ctx, templateURI)
	if err != nil {
		return fmt.Errorf("connect to template: %w", err)
	}
	defer func() { _ = templateConn.Close(ctx) }()

	// Tests always apply the embeddings migration so the embeddings
	// integration suite can exercise the schema. Production gates this
	// via cfg.Embeddings.Enabled — see NewPostgreSQL.
	mig := pkgdb.NewMigrator(templateConn, MigratorConfig(true))
	if err := mig.Migrate(ctx); err != nil {
		return fmt.Errorf("apply v1alpha1 migrations: %w", err)
	}
	return nil
}

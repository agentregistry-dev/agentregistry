package database

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/config"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

const templateDBName = "agent_registry_test_template"

// testSession is a mock session for testing that has full permissions
type testSession struct{}

func (s *testSession) Principal() auth.Principal {
	return auth.Principal{
		User: auth.User{
			Permissions: []auth.Permission{
				{
					Action:          auth.PermissionActionEdit,
					ResourcePattern: "*", // Allow all resources
				},
			},
		},
	}
}

// WithTestSession adds a test session to the context that has full permissions
func WithTestSession(ctx context.Context) context.Context {
	return auth.AuthSessionTo(ctx, &testSession{})
}

// createTestAuthz creates a permissive authorizer for testing
func createTestAuthz() auth.Authorizer {
	// Generate a proper Ed25519 seed for testing
	testSeed := make([]byte, ed25519.SeedSize)
	_, err := rand.Read(testSeed)
	if err != nil {
		panic(fmt.Sprintf("failed to generate test seed: %v", err))
	}

	cfg := &config.Config{
		JWTPrivateKey:            hex.EncodeToString(testSeed),
		EnableRegistryValidation: false, // disable registry validation for testing
	}
	jwtManager := auth.NewJWTManager(cfg)
	authzProvider := auth.NewPublicAuthzProvider(jwtManager)
	return auth.Authorizer{Authz: authzProvider}
}

// ensureTemplateDB creates a template database with migrations applied
// Multiple processes may call this, so we handle race conditions
func ensureTemplateDB(ctx context.Context, adminConn *pgx.Conn) error {
	// Serialize template creation/migration across concurrent test processes to
	// avoid racing on extension creation (which can violate pg_extension_name_index).
	// Use a global advisory lock key to coordinate.
	const lockKey int64 = 0x61726567 // "areg" prefix
	if _, err := adminConn.Exec(ctx, "SELECT pg_advisory_lock($1)", lockKey); err != nil {
		return fmt.Errorf("failed to acquire advisory lock for template DB: %w", err)
	}
	defer func() {
		_, _ = adminConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", lockKey)
	}()

	// Check if template exists
	var exists bool
	err := adminConn.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", templateDBName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check template database: %w", err)
	}

	if !exists { //nolint:nestif
		_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s", templateDBName))
		if err != nil {
			// Ignore duplicate database name error - another process created it concurrently
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) {
				if (pgErr.Code == "42P04") || (pgErr.Code == "23505" && pgErr.ConstraintName == "pg_database_datname_index") {
					// Template got created concurrently; treat as success
				} else {
					return fmt.Errorf("failed to create template database: %w", err)
				}
			} else {
				return fmt.Errorf("failed to create template database: %w", err)
			}
		}
	}

	templateURI := fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", templateDBName)
	if err := ensureVectorExtension(ctx, templateURI); err != nil {
		return err
	}

	// Connect to template and run migrations (always) to keep it up-to-date
	// Create a permissive authz for tests
	testAuthz := createTestAuthz()
	templateDB, err := NewPostgreSQL(ctx, templateURI, testAuthz)
	if err != nil {
		return fmt.Errorf("failed to connect to template database: %w", err)
	}
	defer func() { _ = templateDB.Close() }()

	return nil
}

func ensureVectorExtension(ctx context.Context, uri string) error {
	conn, err := pgx.Connect(ctx, uri)
	if err != nil {
		return fmt.Errorf("failed to connect to template database for extension install: %w", err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS vector"); err != nil {
		return fmt.Errorf("failed to install pgvector extension: %w", err)
	}
	return nil
}

// NewTestDB creates an isolated PostgreSQL database for each test by copying a template.
// The template database has migrations pre-applied, so each test is fast.
// Requires PostgreSQL to be running on localhost:5432 (e.g., via docker-compose).
func NewTestDB(t *testing.T) database.Database {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to postgres database
	adminURI := "postgres://agentregistry:agentregistry@localhost:5432/postgres?sslmode=disable"
	adminConn, err := pgx.Connect(ctx, adminURI)
	if err != nil {
		t.Skipf("PostgreSQL not available (this is expected on macOS GitHub runners). Skipping database tests. Error: %v", err)
	}
	defer func() { _ = adminConn.Close(ctx) }()

	// Ensure template database exists with migrations
	err = ensureTemplateDB(ctx, adminConn)
	require.NoError(t, err, "Failed to initialize template database")

	// Generate unique database name for this test
	var randomBytes [8]byte
	_, err = rand.Read(randomBytes[:])
	require.NoError(t, err, "Failed to generate random database id")
	randomInt := binary.BigEndian.Uint64(randomBytes[:])
	dbName := fmt.Sprintf("test_%d", randomInt)

	// Create test database from template (fast - just copies files)
	_, err = adminConn.Exec(ctx, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", dbName, templateDBName))
	require.NoError(t, err, "Failed to create test database from template")

	// Register cleanup to drop database
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()

		// Terminate any remaining connections
		_, _ = adminConn.Exec(cleanupCtx, fmt.Sprintf(
			"SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = '%s' AND pid <> pg_backend_pid()",
			dbName,
		))

		// Drop database
		_, _ = adminConn.Exec(cleanupCtx, fmt.Sprintf("DROP DATABASE IF EXISTS %s", dbName))
	})

	// Connect to test database (no migrations needed - copied from template)
	testURI := fmt.Sprintf("postgres://agentregistry:agentregistry@localhost:5432/%s?sslmode=disable", dbName)

	// Create a permissive authz for tests
	testAuthz := createTestAuthz()
	db, err := NewPostgreSQL(ctx, testURI, testAuthz)
	require.NoError(t, err, "Failed to connect to test database")

	// Register cleanup to close connection
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Logf("Warning: failed to close test database connection: %v", err)
		}
	})

	return db
}

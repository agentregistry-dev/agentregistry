package database

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// PostgreSQL is the root PostgreSQL-backed store. It owns the pool, authz, and
// transaction orchestration, while domain-specific repository structs own CRUD.
type PostgreSQL struct {
	pool  *pgxpool.Pool
	authz auth.Authorizer
}

type repositoryBase struct {
	executor Executor
	authz    auth.Authorizer
}

type postgresScope struct {
	servers     *serverStore
	providers   *providerStore
	agents      *agentStore
	skills      *skillStore
	prompts     *promptStore
	deployments *deploymentStore
}

var _ database.Scope = (*postgresScope)(nil)

type commandTagAdapter struct {
	tag pgconn.CommandTag
}

func (c commandTagAdapter) RowsAffected() int64 {
	return c.tag.RowsAffected()
}

type rowsAdapter struct {
	rows pgx.Rows
}

func (r rowsAdapter) Close() {
	r.rows.Close()
}

func (r rowsAdapter) Err() error {
	return r.rows.Err()
}

func (r rowsAdapter) Next() bool {
	return r.rows.Next()
}

func (r rowsAdapter) Scan(dest ...any) error {
	return r.rows.Scan(dest...)
}

type rowAdapter struct {
	row pgx.Row
}

func (r rowAdapter) Scan(dest ...any) error {
	return r.row.Scan(dest...)
}

type transactionAdapter struct {
	tx pgx.Tx
}

func (t transactionAdapter) Exec(ctx context.Context, sql string, arguments ...any) (database.CommandTag, error) {
	result, err := t.tx.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, err
	}
	return commandTagAdapter{tag: result}, nil
}

func (t transactionAdapter) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	rows, err := t.tx.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rowsAdapter{rows: rows}, nil
}

func (t transactionAdapter) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return rowAdapter{row: t.tx.QueryRow(ctx, sql, args...)}
}

type poolExecutor struct {
	pool *pgxpool.Pool
}

func (p poolExecutor) Exec(ctx context.Context, sql string, arguments ...any) (database.CommandTag, error) {
	result, err := p.pool.Exec(ctx, sql, arguments...)
	if err != nil {
		return nil, err
	}
	return commandTagAdapter{tag: result}, nil
}

func (p poolExecutor) Query(ctx context.Context, sql string, args ...any) (database.Rows, error) {
	rows, err := p.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return rowsAdapter{rows: rows}, nil
}

func (p poolExecutor) QueryRow(ctx context.Context, sql string, args ...any) database.Row {
	return rowAdapter{row: p.pool.QueryRow(ctx, sql, args...)}
}

// Executor is an internal query surface used by repository methods.
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (database.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (database.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) database.Row
}

func newPostgresScope(executor Executor, authz auth.Authorizer, tx pgx.Tx) *postgresScope {
	base := repositoryBase{executor: executor, authz: authz}
	return &postgresScope{
		servers:     &serverStore{repositoryBase: base, tx: tx},
		providers:   &providerStore{executor: executor},
		agents:      &agentStore{repositoryBase: base},
		skills:      &skillStore{repositoryBase: base},
		prompts:     &promptStore{repositoryBase: base},
		deployments: &deploymentStore{repositoryBase: base},
	}
}

func (s *postgresScope) Servers() database.ServerStore {
	return s.servers
}

func (s *postgresScope) Providers() database.ProviderStore {
	return s.providers
}

func (s *postgresScope) Agents() database.AgentStore {
	return s.agents
}

func (s *postgresScope) Skills() database.SkillStore {
	return s.skills
}

func (s *postgresScope) Prompts() database.PromptStore {
	return s.prompts
}

func (s *postgresScope) Deployments() database.DeploymentStore {
	return s.deployments
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string, authz auth.Authorizer, vectorEnabled bool) (*PostgreSQL, error) {
	// Parse connection config for pool settings
	config, err := pgxpool.ParseConfig(connectionURI)
	if err != nil {
		return nil, fmt.Errorf("failed to parse PostgreSQL config: %w", err)
	}

	// Configure pool for stability-focused defaults
	config.MaxConns = 30                      // Handle good concurrent load
	config.MinConns = 5                       // Keep connections warm for fast response
	config.MaxConnIdleTime = 30 * time.Minute // Keep connections available for bursts
	config.MaxConnLifetime = 2 * time.Hour    // Refresh connections regularly for stability

	// Create connection pool with configured settings
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create PostgreSQL pool: %w", err)
	}

	// Test the connection
	if err = pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping PostgreSQL: %w", err)
	}

	// Run migrations using a single connection from the pool
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire connection for migrations: %w", err)
	}
	defer conn.Release()

	migrator := database.NewMigrator(conn.Conn(), DefaultMigratorConfig())
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	if vectorEnabled {
		vectorMigrator := database.NewMigrator(conn.Conn(), VectorMigratorConfig())
		if err := vectorMigrator.Migrate(ctx); err != nil {
			return nil, fmt.Errorf("failed to run vector database migrations: %w", err)
		}
	}

	return &PostgreSQL{pool: pool, authz: authz}, nil
}

func (db *PostgreSQL) scope() *postgresScope {
	return newPostgresScope(poolExecutor{pool: db.pool}, db.authz, nil)
}

func (db *PostgreSQL) Servers() database.ServerStore {
	return db.scope().Servers()
}

func (db *PostgreSQL) Providers() database.ProviderStore {
	return db.scope().Providers()
}

func (db *PostgreSQL) Agents() database.AgentStore {
	return db.scope().Agents()
}

func (db *PostgreSQL) Skills() database.SkillStore {
	return db.scope().Skills()
}

func (db *PostgreSQL) Prompts() database.PromptStore {
	return db.scope().Prompts()
}

func (db *PostgreSQL) Deployments() database.DeploymentStore {
	return db.scope().Deployments()
}

// InTransaction executes a function within a database transaction
func (db *PostgreSQL) InTransaction(ctx context.Context, fn func(ctx context.Context, scope database.Scope) error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	//nolint:contextcheck // Intentionally using separate context for rollback to ensure cleanup even if request is cancelled
	defer func() {
		rollbackCtx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
		defer cancel()
		if rbErr := tx.Rollback(rollbackCtx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			slog.Error("failed to rollback transaction", "error", rbErr)
		}
	}()

	txScope := newPostgresScope(transactionAdapter{tx: tx}, db.authz, tx)
	if err := fn(ctx, txScope); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}

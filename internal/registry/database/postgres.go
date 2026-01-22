package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	models "github.com/agentregistry-dev/agentregistry/internal/models"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

// PostgreSQL is an implementation of the Database interface using PostgreSQL
type PostgreSQL struct {
	pool *pgxpool.Pool
}

const semanticMetadataKey = "agentregistry.solo.io/semantic"

// Executor is an interface for executing queries (satisfied by both pgx.Tx and pgxpool.Pool)
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// getExecutor returns the appropriate executor (transaction or pool)
func (db *PostgreSQL) getExecutor(tx pgx.Tx) Executor {
	if tx != nil {
		return tx
	}
	return db.pool
}

// NewPostgreSQL creates a new instance of the PostgreSQL database
func NewPostgreSQL(ctx context.Context, connectionURI string) (*PostgreSQL, error) {
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

	migrator := NewMigrator(conn.Conn())
	if err := migrator.Migrate(ctx); err != nil {
		return nil, fmt.Errorf("failed to run database migrations: %w", err)
	}

	return &PostgreSQL{
		pool: pool,
	}, nil
}

func (db *PostgreSQL) ListServers(
	ctx context.Context,
	tx pgx.Tx,
	filter *ServerFilter,
	cursor string,
	limit int,
) ([]*apiv0.ServerResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}

	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	semanticActive := filter != nil && filter.Semantic != nil && len(filter.Semantic.QueryEmbedding) > 0
	var semanticLiteral string
	if semanticActive {
		var err error
		semanticLiteral, err = vectorLiteral(filter.Semantic.QueryEmbedding)
		if err != nil {
			return nil, "", fmt.Errorf("invalid semantic embedding: %w", err)
		}
	}

	var whereConditions []string
	args := []any{}
	argIndex := 1

	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("version = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
		if filter.Published != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("published = $%d", argIndex))
			args = append(args, *filter.Published)
			argIndex++
		}
	}

	if semanticActive {
		whereConditions = append(whereConditions, "semantic_embedding IS NOT NULL")
	}

	if cursor != "" && !semanticActive {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) == 2 {
			cursorServerName := parts[0]
			cursorVersion := parts[1]
			whereConditions = append(whereConditions, fmt.Sprintf("(server_name > $%d OR (server_name = $%d AND version > $%d))", argIndex, argIndex+1, argIndex+2))
			args = append(args, cursorServerName, cursorServerName, cursorVersion)
			argIndex += 3
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("server_name > $%d", argIndex))
			args = append(args, cursor)
			argIndex++
		}
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	selectClause := `
        SELECT server_name, version, status, published, published_at, updated_at, is_latest, value`
	orderClause := "ORDER BY server_name, version"

	if semanticActive {
		selectClause += fmt.Sprintf(", semantic_embedding <=> $%d::vector AS semantic_score", argIndex)
		args = append(args, semanticLiteral)
		vectorParamIdx := argIndex
		argIndex++

		if filter.Semantic.Threshold > 0 {
			whereClauseCondition := fmt.Sprintf("semantic_embedding <=> $%d::vector <= $%d", vectorParamIdx, argIndex)
			if whereClause == "" {
				whereClause = "WHERE " + whereClauseCondition
			} else {
				whereClause += " AND " + whereClauseCondition
			}
			args = append(args, filter.Semantic.Threshold)
			argIndex++
		}
		orderClause = "ORDER BY semantic_score ASC, server_name, version"
	}

	query := fmt.Sprintf(`
        %s
        FROM servers
        %s
        %s
        LIMIT $%d
    `, selectClause, whereClause, orderClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var serverName, version, status string
		var published, isLatest bool
		var publishedAt, updatedAt time.Time
		var valueJSON []byte
		var semanticScore sql.NullFloat64

		var scanErr error
		if semanticActive {
			scanErr = rows.Scan(&serverName, &version, &status, &published, &publishedAt, &updatedAt, &isLatest, &valueJSON, &semanticScore)
		} else {
			scanErr = rows.Scan(&serverName, &version, &status, &published, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		}
		if scanErr != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", scanErr)
		}

		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		if semanticActive && semanticScore.Valid {
			annotateServerSemanticScore(&serverJSON, semanticScore.Float64)
		}

		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating rows: %w", err)
	}

	nextCursor := ""
	if !semanticActive && len(results) > 0 && len(results) >= limit {
		lastResult := results[len(results)-1]
		nextCursor = lastResult.Server.Name + ":" + lastResult.Server.Version
	}

	return results, nextCursor, nil
}

func annotateServerSemanticScore(server *apiv0.ServerJSON, score float64) {
	if server == nil {
		return
	}
	if server.Meta == nil {
		server.Meta = &apiv0.ServerMeta{}
	}
	if server.Meta.PublisherProvided == nil {
		server.Meta.PublisherProvided = map[string]any{}
	}
	server.Meta.PublisherProvided[semanticMetadataKey] = map[string]any{
		"score": score,
	}
}

// GetServerByName retrieves the latest version of a server by server name
func (db *PostgreSQL) GetServerByName(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, published, value
		FROM servers
		WHERE server_name = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`

	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest, published bool
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name: %w", err)
	}

	// Parse the ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetServerByNameAndVersion retrieves a specific version of a server by server name and version
func (db *PostgreSQL) GetServerByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string, publishedOnly bool) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND version = $2
	`

	if publishedOnly {
		query += ` AND published = true`
	}

	query += `
		ORDER BY published_at DESC
		LIMIT 1
	`

	var name, vers, status string
	var published, isLatest bool
	var publishedAt, updatedAt time.Time
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, serverName, version).Scan(&name, &vers, &status, &published, &publishedAt, &updatedAt, &isLatest, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}

	// Parse the ServerJSON from JSONB
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// GetAllVersionsByServerName retrieves all versions of a server by server name
// If publishedOnly is true, only returns versions where published = true
func (db *PostgreSQL) GetAllVersionsByServerName(ctx context.Context, tx pgx.Tx, serverName string, publishedOnly bool) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	query := `
		SELECT server_name, version, status, published, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1`

	if publishedOnly {
		query += ` AND published = true`
	}

	query += `
		ORDER BY published_at DESC
	`

	rows, err := db.getExecutor(tx).Query(ctx, query, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, version, status string
		var published, isLatest bool
		var publishedAt, updatedAt time.Time
		var valueJSON []byte

		err := rows.Scan(&name, &version, &status, &published, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		// Parse the ServerJSON from JSONB
		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		// Build ServerResponse with separated metadata
		serverResponse := &apiv0.ServerResponse{
			Server: serverJSON,
			Meta: apiv0.ResponseMeta{
				Official: &apiv0.RegistryExtensions{
					Status:      model.Status(status),
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}

		results = append(results, serverResponse)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, ErrNotFound
	}

	return results, nil
}

// CreateServer inserts a new server version with official metadata
func (db *PostgreSQL) CreateServer(ctx context.Context, tx pgx.Tx, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate inputs
	if serverJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("serverJSON and officialMeta are required")
	}

	if serverJSON.Name == "" || serverJSON.Version == "" {
		return nil, fmt.Errorf("server name and version are required")
	}

	// Marshal the ServerJSON to JSONB
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	// Insert the new server version using composite primary key
	insertQuery := `
		INSERT INTO servers (server_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = db.getExecutor(tx).Exec(ctx, insertQuery,
		serverJSON.Name,
		serverJSON.Version,
		string(officialMeta.Status),
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		officialMeta.IsLatest,
		valueJSON,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to insert server: %w", err)
	}

	// Return the complete ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: officialMeta,
		},
	}

	return serverResponse, nil
}

// UpdateServer updates an existing server record with new server details
func (db *PostgreSQL) UpdateServer(ctx context.Context, tx pgx.Tx, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Validate inputs
	if serverJSON == nil {
		return nil, fmt.Errorf("serverJSON is required")
	}

	// Ensure the serverJSON matches the provided serverName and version
	if serverJSON.Name != serverName || serverJSON.Version != version {
		return nil, fmt.Errorf("%w: server name and version in JSON must match parameters", ErrInvalidInput)
	}

	// Marshal updated ServerJSON
	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	// Update only the JSON data (keep existing metadata columns)
	query := `
		UPDATE servers
		SET value = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, published, published_at, updated_at, is_latest
	`

	var name, vers, status string
	var published, isLatest bool
	var publishedAt, updatedAt time.Time

	err = db.getExecutor(tx).QueryRow(ctx, query, valueJSON, serverName, version).Scan(&name, &vers, &status, &published, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

	// Return the updated ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(status),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// SetServerStatus updates the status of a specific server version
func (db *PostgreSQL) SetServerStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	// Update the status column
	query := `
		UPDATE servers
		SET status = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, published, value, published_at, updated_at, is_latest
	`

	var name, vers, currentStatus string
	var published, isLatest bool
	var publishedAt, updatedAt time.Time
	var valueJSON []byte

	err := db.getExecutor(tx).QueryRow(ctx, query, status, serverName, version).Scan(&name, &vers, &currentStatus, &published, &valueJSON, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server status: %w", err)
	}

	// Unmarshal the JSON data
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Return the updated ServerResponse
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				Status:      model.Status(currentStatus),
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// InTransaction executes a function within a database transaction
func (db *PostgreSQL) InTransaction(ctx context.Context, fn func(ctx context.Context, tx pgx.Tx) error) error {
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
			log.Printf("failed to rollback transaction: %v", rbErr)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// AcquirePublishLock acquires an exclusive advisory lock for publishing a server
// This prevents race conditions when multiple versions are published concurrently
// Using pg_advisory_xact_lock which auto-releases on transaction end
func (db *PostgreSQL) AcquirePublishLock(ctx context.Context, tx pgx.Tx, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	lockID := hashServerName(serverName)

	if _, err := db.getExecutor(tx).Exec(ctx, "SELECT pg_advisory_xact_lock($1)", lockID); err != nil {
		return fmt.Errorf("failed to acquire publish lock: %w", err)
	}

	return nil
}

// hashServerName creates a consistent hash of the server name for advisory locking
// We use FNV-1a hash and mask to 63 bits to fit in PostgreSQL's bigint range
func hashServerName(name string) int64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(name); i++ {
		hash ^= uint64(name[i])
		hash *= prime64
	}
	//nolint:gosec // Intentional conversion with masking to 63 bits
	return int64(hash & 0x7FFFFFFFFFFFFFFF)
}

// GetCurrentLatestVersion retrieves the current latest version of a server by server name
func (db *PostgreSQL) GetCurrentLatestVersion(ctx context.Context, tx pgx.Tx, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `
		SELECT server_name, version, status, published, value, published_at, updated_at, is_latest
		FROM servers
		WHERE server_name = $1 AND is_latest = true
	`

	row := executor.QueryRow(ctx, query, serverName)

	var name, version, status string
	var published, isLatest bool
	var publishedAt, updatedAt time.Time
	var jsonValue []byte

	err := row.Scan(&name, &version, &status, &published, &jsonValue, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan server row: %w", err)
	}

	// Parse the JSON value to get the server details
	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(jsonValue, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

	// Build ServerResponse with separated metadata
	serverResponse := &apiv0.ServerResponse{
		Server: serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: &apiv0.RegistryExtensions{
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}

	return serverResponse, nil
}

// CountServerVersions counts the number of versions for a server
func (db *PostgreSQL) CountServerVersions(ctx context.Context, tx pgx.Tx, serverName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `SELECT COUNT(*) FROM servers WHERE server_name = $1`

	var count int
	err := executor.QueryRow(ctx, query, serverName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count server versions: %w", err)
	}

	return count, nil
}

// CheckVersionExists checks if a specific version exists for a server
func (db *PostgreSQL) CheckVersionExists(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `SELECT EXISTS(SELECT 1 FROM servers WHERE server_name = $1 AND version = $2)`

	var exists bool
	err := executor.QueryRow(ctx, query, serverName, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check version existence: %w", err)
	}

	return exists, nil
}

// UnmarkAsLatest marks the current latest version of a server as no longer latest
func (db *PostgreSQL) UnmarkAsLatest(ctx context.Context, tx pgx.Tx, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)

	query := `UPDATE servers SET is_latest = false WHERE server_name = $1 AND is_latest = true`

	_, err := executor.Exec(ctx, query, serverName)
	if err != nil {
		return fmt.Errorf("failed to unmark latest version: %w", err)
	}

	return nil
}

// PublishServer marks a server as published
func (db *PostgreSQL) PublishServer(ctx context.Context, tx pgx.Tx, serverName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE servers SET published = true, published_date = NOW() WHERE server_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to publish server: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UnpublishServer marks a server as unpublished
func (db *PostgreSQL) UnpublishServer(ctx context.Context, tx pgx.Tx, serverName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE servers SET published = false, unpublished_date = NOW() WHERE server_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to unpublish server: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteServer permanently removes a server version from the database
func (db *PostgreSQL) DeleteServer(ctx context.Context, tx pgx.Tx, serverName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `DELETE FROM servers WHERE server_name = $1 AND version = $2`
	result, err := executor.Exec(ctx, query, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// IsServerPublished checks if a server is published
func (db *PostgreSQL) IsServerPublished(ctx context.Context, tx pgx.Tx, serverName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `SELECT published FROM servers WHERE server_name = $1 AND version = $2`

	var published bool
	err := executor.QueryRow(ctx, query, serverName, version).Scan(&published)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("failed to check if server is published: %w", err)
	}

	return published, nil
}

// SetServerEmbedding stores semantic embedding metadata for a server version.
func (db *PostgreSQL) SetServerEmbedding(ctx context.Context, tx pgx.Tx, serverName, version string, embedding *SemanticEmbedding) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)

	var (
		query string
		args  []any
	)

	if embedding == nil || len(embedding.Vector) == 0 {
		query = `
			UPDATE servers
			SET semantic_embedding = NULL,
			    semantic_embedding_provider = NULL,
			    semantic_embedding_model = NULL,
			    semantic_embedding_dimensions = NULL,
			    semantic_embedding_checksum = NULL,
			    semantic_embedding_generated_at = NULL
			WHERE server_name = $1 AND version = $2
		`
		args = []any{serverName, version}
	} else {
		vectorLiteral, err := vectorLiteral(embedding.Vector)
		if err != nil {
			return err
		}
		query = `
			UPDATE servers
			SET semantic_embedding = $3::vector,
			    semantic_embedding_provider = $4,
			    semantic_embedding_model = $5,
			    semantic_embedding_dimensions = $6,
			    semantic_embedding_checksum = $7,
			    semantic_embedding_generated_at = $8
			WHERE server_name = $1 AND version = $2
		`
		args = []any{
			serverName,
			version,
			vectorLiteral,
			embedding.Provider,
			embedding.Model,
			embedding.Dimensions,
			embedding.Checksum,
			embedding.Generated,
		}
	}

	result, err := executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update server embedding: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetServerEmbeddingMetadata retrieves embedding metadata for a server version without loading
// the underlying vector payload. This is useful for maintenance tasks that only need to know
// whether an embedding exists or if its checksum is stale.
func (db *PostgreSQL) GetServerEmbeddingMetadata(ctx context.Context, tx pgx.Tx, serverName, version string) (*SemanticEmbeddingMetadata, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `
		SELECT
			semantic_embedding IS NOT NULL AS has_embedding,
			semantic_embedding_provider,
			semantic_embedding_model,
			semantic_embedding_dimensions,
			semantic_embedding_checksum,
			semantic_embedding_generated_at
		FROM servers
		WHERE server_name = $1 AND version = $2
		LIMIT 1
	`

	var (
		hasEmbedding bool
		provider     sql.NullString
		model        sql.NullString
		dimensions   sql.NullInt32
		checksum     sql.NullString
		generatedAt  sql.NullTime
	)

	err := executor.QueryRow(ctx, query, serverName, version).Scan(
		&hasEmbedding,
		&provider,
		&model,
		&dimensions,
		&checksum,
		&generatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch server embedding metadata: %w", err)
	}

	meta := &SemanticEmbeddingMetadata{
		HasEmbedding: hasEmbedding,
	}
	if provider.Valid {
		meta.Provider = provider.String
	}
	if model.Valid {
		meta.Model = model.String
	}
	if dimensions.Valid {
		meta.Dimensions = int(dimensions.Int32)
	}
	if checksum.Valid {
		meta.Checksum = checksum.String
	}
	if generatedAt.Valid {
		meta.Generated = generatedAt.Time
	}

	return meta, nil
}

func (db *PostgreSQL) UpsertServerReadme(ctx context.Context, tx pgx.Tx, readme *ServerReadme) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if readme == nil {
		return fmt.Errorf("readme is required")
	}
	if readme.ServerName == "" || readme.Version == "" {
		return fmt.Errorf("server name and version are required")
	}
	if readme.ContentType == "" {
		readme.ContentType = "text/markdown"
	}

	if readme.SizeBytes == 0 {
		readme.SizeBytes = len(readme.Content)
	}
	if len(readme.SHA256) == 0 {
		sum := sha256.Sum256(readme.Content)
		readme.SHA256 = sum[:]
	}
	if readme.FetchedAt.IsZero() {
		readme.FetchedAt = time.Now()
	}

	executor := db.getExecutor(tx)
	query := `
        INSERT INTO server_readmes (server_name, version, content, content_type, size_bytes, sha256, fetched_at)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
        ON CONFLICT (server_name, version) DO UPDATE
        SET content = EXCLUDED.content,
            content_type = EXCLUDED.content_type,
            size_bytes = EXCLUDED.size_bytes,
            sha256 = EXCLUDED.sha256,
            fetched_at = EXCLUDED.fetched_at
    `

	if _, err := executor.Exec(ctx, query,
		readme.ServerName,
		readme.Version,
		readme.Content,
		readme.ContentType,
		readme.SizeBytes,
		readme.SHA256,
		readme.FetchedAt,
	); err != nil {
		return fmt.Errorf("failed to upsert server readme: %w", err)
	}

	return nil
}

func (db *PostgreSQL) GetServerReadme(ctx context.Context, tx pgx.Tx, serverName, version string) (*ServerReadme, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `
        SELECT server_name, version, content, content_type, size_bytes, sha256, fetched_at
        FROM server_readmes
        WHERE server_name = $1 AND version = $2
        LIMIT 1
    `

	row := executor.QueryRow(ctx, query, serverName, version)
	return scanServerReadme(row)
}

func (db *PostgreSQL) GetLatestServerReadme(ctx context.Context, tx pgx.Tx, serverName string) (*ServerReadme, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `
        SELECT sr.server_name, sr.version, sr.content, sr.content_type, sr.size_bytes, sr.sha256, sr.fetched_at
        FROM server_readmes sr
        INNER JOIN servers s ON sr.server_name = s.server_name AND sr.version = s.version
        WHERE sr.server_name = $1 AND s.is_latest = true
        LIMIT 1
    `

	row := executor.QueryRow(ctx, query, serverName)
	return scanServerReadme(row)
}

func scanServerReadme(row pgx.Row) (*ServerReadme, error) {
	var readme ServerReadme
	if err := row.Scan(
		&readme.ServerName,
		&readme.Version,
		&readme.Content,
		&readme.ContentType,
		&readme.SizeBytes,
		&readme.SHA256,
		&readme.FetchedAt,
	); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan server readme: %w", err)
	}
	return &readme, nil
}

// ==============================
// Agents implementations
// ==============================

// ListAgents returns paginated agents with filtering
func (db *PostgreSQL) ListAgents(ctx context.Context, tx pgx.Tx, filter *AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	semanticActive := filter != nil && filter.Semantic != nil && len(filter.Semantic.QueryEmbedding) > 0
	var semanticLiteral string
	if semanticActive {
		var err error
		semanticLiteral, err = vectorLiteral(filter.Semantic.QueryEmbedding)
		if err != nil {
			return nil, "", fmt.Errorf("invalid semantic embedding: %w", err)
		}
	}

	var whereConditions []string
	args := []any{}
	argIndex := 1

	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("agent_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("agent_name ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("version = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
		if filter.Published != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("published = $%d", argIndex))
			args = append(args, *filter.Published)
			argIndex++
		}
	}

	if semanticActive {
		whereConditions = append(whereConditions, "semantic_embedding IS NOT NULL")
	}

	if cursor != "" && !semanticActive {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) == 2 {
			cursorName := parts[0]
			cursorVersion := parts[1]
			whereConditions = append(whereConditions, fmt.Sprintf("(agent_name > $%d OR (agent_name = $%d AND version > $%d))", argIndex, argIndex+1, argIndex+2))
			args = append(args, cursorName, cursorName, cursorVersion)
			argIndex += 3
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("agent_name > $%d", argIndex))
			args = append(args, cursor)
			argIndex++
		}
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	selectClause := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, published, value`
	orderClause := "ORDER BY agent_name, version"

	if semanticActive {
		selectClause += fmt.Sprintf(", semantic_embedding <=> $%d::vector AS semantic_score", argIndex)
		args = append(args, semanticLiteral)
		vectorParamIdx := argIndex
		argIndex++

		if filter.Semantic.Threshold > 0 {
			condition := fmt.Sprintf("semantic_embedding <=> $%d::vector <= $%d", vectorParamIdx, argIndex)
			if whereClause == "" {
				whereClause = "WHERE " + condition
			} else {
				whereClause += " AND " + condition
			}
			args = append(args, filter.Semantic.Threshold)
			argIndex++
		}

		orderClause = "ORDER BY semantic_score ASC, agent_name, version"
	}

	query := fmt.Sprintf(`
		%s
		FROM agents
		%s
		%s
		LIMIT $%d
	`, selectClause, whereClause, orderClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

	var results []*models.AgentResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest, published bool
		var valueJSON []byte
		var semanticScore sql.NullFloat64

		var scanErr error
		if semanticActive {
			scanErr = rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON, &semanticScore)
		} else {
			scanErr = rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON)
		}

		if scanErr != nil {
			return nil, "", fmt.Errorf("failed to scan agent row: %w", err)
		}

		var agentJSON models.AgentJSON
		if err := json.Unmarshal(valueJSON, &agentJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal agent JSON: %w", err)
		}

		resp := &models.AgentResponse{
			Agent: agentJSON,
			Meta: models.AgentResponseMeta{
				Official: &models.AgentRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
					Published:   published,
				},
			},
		}
		if semanticActive && semanticScore.Valid {
			resp.Meta.Semantic = &models.AgentSemanticMeta{
				Score: semanticScore.Float64,
			}
		}
		results = append(results, resp)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating agent rows: %w", err)
	}

	nextCursor := ""
	if !semanticActive && len(results) > 0 && len(results) >= limit {
		last := results[len(results)-1]
		nextCursor = last.Agent.Name + ":" + last.Agent.Version
	}
	return results, nextCursor, nil
}

func (db *PostgreSQL) GetAgentByName(ctx context.Context, tx pgx.Tx, agentName string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, published, value
		FROM agents
		WHERE agent_name = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest, published bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, agentName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get agent by name: %w", err)
	}
	var agentJSON models.AgentJSON
	if err := json.Unmarshal(valueJSON, &agentJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent JSON: %w", err)
	}
	return &models.AgentResponse{
		Agent: agentJSON,
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Published:   published,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetAgentByNameAndVersion(ctx context.Context, tx pgx.Tx, agentName, version string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, value
		FROM agents
		WHERE agent_name = $1 AND version = $2
		LIMIT 1
	`
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, agentName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get agent by name and version: %w", err)
	}
	var agentJSON models.AgentJSON
	if err := json.Unmarshal(valueJSON, &agentJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent JSON: %w", err)
	}
	return &models.AgentResponse{
		Agent: agentJSON,
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetAllVersionsByAgentName(ctx context.Context, tx pgx.Tx, agentName string) ([]*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, value
		FROM agents
		WHERE agent_name = $1
		ORDER BY published_at DESC
	`
	rows, err := db.getExecutor(tx).Query(ctx, query, agentName)
	if err != nil {
		return nil, fmt.Errorf("failed to query agent versions: %w", err)
	}
	defer rows.Close()
	var results []*models.AgentResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte
		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
			return nil, fmt.Errorf("failed to scan agent row: %w", err)
		}
		var agentJSON models.AgentJSON
		if err := json.Unmarshal(valueJSON, &agentJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal agent JSON: %w", err)
		}
		results = append(results, &models.AgentResponse{
			Agent: agentJSON,
			Meta: models.AgentResponseMeta{
				Official: &models.AgentRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating agent rows: %w", err)
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results, nil
}

func (db *PostgreSQL) CreateAgent(ctx context.Context, tx pgx.Tx, agentJSON *models.AgentJSON, officialMeta *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if agentJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("agentJSON and officialMeta are required")
	}
	if agentJSON.Name == "" || agentJSON.Version == "" {
		return nil, fmt.Errorf("agent name and version are required")
	}
	valueJSON, err := json.Marshal(agentJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent JSON: %w", err)
	}
	insert := `
		INSERT INTO agents (agent_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if _, err := db.getExecutor(tx).Exec(ctx, insert,
		agentJSON.Name,
		agentJSON.Version,
		officialMeta.Status,
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		officialMeta.IsLatest,
		valueJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to insert agent: %w", err)
	}
	return &models.AgentResponse{
		Agent: *agentJSON,
		Meta: models.AgentResponseMeta{
			Official: officialMeta,
		},
	}, nil
}

func (db *PostgreSQL) UpdateAgent(ctx context.Context, tx pgx.Tx, agentName, version string, agentJSON *models.AgentJSON) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if agentJSON == nil {
		return nil, fmt.Errorf("agentJSON is required")
	}
	if agentJSON.Name != agentName || agentJSON.Version != version {
		return nil, fmt.Errorf("%w: agent name and version in JSON must match parameters", ErrInvalidInput)
	}
	valueJSON, err := json.Marshal(agentJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated agent: %w", err)
	}
	query := `
		UPDATE agents
		SET value = $1, updated_at = NOW()
		WHERE agent_name = $2 AND version = $3
		RETURNING agent_name, version, status, published_at, updated_at, is_latest
	`
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	if err := db.getExecutor(tx).QueryRow(ctx, query, valueJSON, agentName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update agent: %w", err)
	}
	return &models.AgentResponse{
		Agent: *agentJSON,
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (db *PostgreSQL) SetAgentStatus(ctx context.Context, tx pgx.Tx, agentName, version string, status string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
		UPDATE agents
		SET status = $1, updated_at = NOW()
		WHERE agent_name = $2 AND version = $3
		RETURNING agent_name, version, status, value, published_at, updated_at, is_latest
	`
	var name, vers, currentStatus string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, status, agentName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update agent status: %w", err)
	}
	var agentJSON models.AgentJSON
	if err := json.Unmarshal(valueJSON, &agentJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent JSON: %w", err)
	}
	return &models.AgentResponse{
		Agent: agentJSON,
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				Status:      currentStatus,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetCurrentLatestAgentVersion(ctx context.Context, tx pgx.Tx, agentName string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `
		SELECT agent_name, version, status, value, published_at, updated_at, is_latest
		FROM agents
		WHERE agent_name = $1 AND is_latest = true
	`
	row := executor.QueryRow(ctx, query, agentName)
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte
	if err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan agent row: %w", err)
	}
	var agentJSON models.AgentJSON
	if err := json.Unmarshal(jsonValue, &agentJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal agent JSON: %w", err)
	}
	return &models.AgentResponse{
		Agent: agentJSON,
		Meta: models.AgentResponseMeta{
			Official: &models.AgentRegistryExtensions{
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Status:      status,
			},
		},
	}, nil
}

func (db *PostgreSQL) CountAgentVersions(ctx context.Context, tx pgx.Tx, agentName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `SELECT COUNT(*) FROM agents WHERE agent_name = $1`
	var count int
	if err := executor.QueryRow(ctx, query, agentName).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count agent versions: %w", err)
	}
	return count, nil
}

func (db *PostgreSQL) CheckAgentVersionExists(ctx context.Context, tx pgx.Tx, agentName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `SELECT EXISTS(SELECT 1 FROM agents WHERE agent_name = $1 AND version = $2)`
	var exists bool
	if err := executor.QueryRow(ctx, query, agentName, version).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check agent version existence: %w", err)
	}
	return exists, nil
}

func (db *PostgreSQL) UnmarkAgentAsLatest(ctx context.Context, tx pgx.Tx, agentName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `UPDATE agents SET is_latest = false WHERE agent_name = $1 AND is_latest = true`
	if _, err := executor.Exec(ctx, query, agentName); err != nil {
		return fmt.Errorf("failed to unmark latest agent version: %w", err)
	}
	return nil
}

// PublishAgent marks an agent as published
func (db *PostgreSQL) PublishAgent(ctx context.Context, tx pgx.Tx, agentName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE agents SET published = true, published_date = NOW() WHERE agent_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, agentName, version)
	if err != nil {
		return fmt.Errorf("failed to publish agent: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UnpublishAgent marks an agent as unpublished
func (db *PostgreSQL) UnpublishAgent(ctx context.Context, tx pgx.Tx, agentName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE agents SET published = false, unpublished_date = NOW() WHERE agent_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, agentName, version)
	if err != nil {
		return fmt.Errorf("failed to unpublish agent: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// IsAgentPublished checks if an agent is published
func (db *PostgreSQL) IsAgentPublished(ctx context.Context, tx pgx.Tx, agentName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `SELECT published FROM agents WHERE agent_name = $1 AND version = $2`

	var published bool
	err := executor.QueryRow(ctx, query, agentName, version).Scan(&published)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("failed to check if agent is published: %w", err)
	}

	return published, nil
}

// SetAgentEmbedding stores semantic embedding metadata for an agent version.
func (db *PostgreSQL) SetAgentEmbedding(ctx context.Context, tx pgx.Tx, agentName, version string, embedding *SemanticEmbedding) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)

	var (
		query string
		args  []any
	)

	if embedding == nil || len(embedding.Vector) == 0 {
		query = `
			UPDATE agents
			SET semantic_embedding = NULL,
			    semantic_embedding_provider = NULL,
			    semantic_embedding_model = NULL,
			    semantic_embedding_dimensions = NULL,
			    semantic_embedding_checksum = NULL,
			    semantic_embedding_generated_at = NULL
			WHERE agent_name = $1 AND version = $2
		`
		args = []any{agentName, version}
	} else {
		vectorLiteral, err := vectorLiteral(embedding.Vector)
		if err != nil {
			return err
		}
		query = `
			UPDATE agents
			SET semantic_embedding = $3::vector,
			    semantic_embedding_provider = $4,
			    semantic_embedding_model = $5,
			    semantic_embedding_dimensions = $6,
			    semantic_embedding_checksum = $7,
			    semantic_embedding_generated_at = $8
			WHERE agent_name = $1 AND version = $2
		`
		args = []any{
			agentName,
			version,
			vectorLiteral,
			embedding.Provider,
			embedding.Model,
			embedding.Dimensions,
			embedding.Checksum,
			embedding.Generated,
		}
	}

	result, err := executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update agent embedding: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// GetAgentEmbeddingMetadata retrieves embedding metadata for an agent version without loading the vector.
func (db *PostgreSQL) GetAgentEmbeddingMetadata(ctx context.Context, tx pgx.Tx, agentName, version string) (*SemanticEmbeddingMetadata, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `
		SELECT
			semantic_embedding IS NOT NULL AS has_embedding,
			semantic_embedding_provider,
			semantic_embedding_model,
			semantic_embedding_dimensions,
			semantic_embedding_checksum,
			semantic_embedding_generated_at
		FROM agents
		WHERE agent_name = $1 AND version = $2
		LIMIT 1
	`

	var (
		hasEmbedding bool
		provider     sql.NullString
		model        sql.NullString
		dimensions   sql.NullInt32
		checksum     sql.NullString
		generatedAt  sql.NullTime
	)

	err := executor.QueryRow(ctx, query, agentName, version).Scan(
		&hasEmbedding,
		&provider,
		&model,
		&dimensions,
		&checksum,
		&generatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch agent embedding metadata: %w", err)
	}

	meta := &SemanticEmbeddingMetadata{
		HasEmbedding: hasEmbedding,
	}
	if provider.Valid {
		meta.Provider = provider.String
	}
	if model.Valid {
		meta.Model = model.String
	}
	if dimensions.Valid {
		meta.Dimensions = int(dimensions.Int32)
	}
	if checksum.Valid {
		meta.Checksum = checksum.String
	}
	if generatedAt.Valid {
		meta.Generated = generatedAt.Time
	}

	return meta, nil
}

// ==============================
// Skills implementations
// ==============================

// ListSkills returns paginated skills with filtering
func (db *PostgreSQL) ListSkills(ctx context.Context, tx pgx.Tx, filter *SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	var whereConditions []string
	args := []any{}
	argIndex := 1

	if filter != nil {
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("skill_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.RemoteURL != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("EXISTS (SELECT 1 FROM jsonb_array_elements(value->'remotes') AS remote WHERE remote->>'url' = $%d)", argIndex))
			args = append(args, *filter.RemoteURL)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("skill_name ILIKE $%d", argIndex))
			args = append(args, "%"+*filter.SubstringName+"%")
			argIndex++
		}
		if filter.Version != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("version = $%d", argIndex))
			args = append(args, *filter.Version)
			argIndex++
		}
		if filter.IsLatest != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("is_latest = $%d", argIndex))
			args = append(args, *filter.IsLatest)
			argIndex++
		}
		if filter.Published != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("published = $%d", argIndex))
			args = append(args, *filter.Published)
			argIndex++
		}
	}

	if cursor != "" {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) == 2 {
			cursorName := parts[0]
			cursorVersion := parts[1]
			whereConditions = append(whereConditions, fmt.Sprintf("(skill_name > $%d OR (skill_name = $%d AND version > $%d))", argIndex, argIndex+1, argIndex+2))
			args = append(args, cursorName, cursorName, cursorVersion)
			argIndex += 3
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("skill_name > $%d", argIndex))
			args = append(args, cursor)
			argIndex++
		}
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
        SELECT skill_name, version, status, published_at, updated_at, is_latest, published, value
        FROM skills
        %s
        ORDER BY skill_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := db.getExecutor(tx).Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query skills: %w", err)
	}
	defer rows.Close()

	var results []*models.SkillResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest, published bool
		var valueJSON []byte

		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON); err != nil {
			return nil, "", fmt.Errorf("failed to scan skill row: %w", err)
		}

		var skillJSON models.SkillJSON
		if err := json.Unmarshal(valueJSON, &skillJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal skill JSON: %w", err)
		}

		resp := &models.SkillResponse{
			Skill: skillJSON,
			Meta: models.SkillResponseMeta{
				Official: &models.SkillRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
					Published:   published,
				},
			},
		}
		results = append(results, resp)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating skill rows: %w", err)
	}

	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		last := results[len(results)-1]
		nextCursor = last.Skill.Name + ":" + last.Skill.Version
	}
	return results, nextCursor, nil
}

func (db *PostgreSQL) GetSkillByName(ctx context.Context, tx pgx.Tx, skillName string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, published, value
        FROM skills
        WHERE skill_name = $1 AND is_latest = true
        ORDER BY published_at DESC
        LIMIT 1
    `
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest, published bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, skillName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get skill by name: %w", err)
	}
	var skillJSON models.SkillJSON
	if err := json.Unmarshal(valueJSON, &skillJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill JSON: %w", err)
	}
	return &models.SkillResponse{
		Skill: skillJSON,
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Published:   published,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetSkillByNameAndVersion(ctx context.Context, tx pgx.Tx, skillName, version string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, published, value
        FROM skills
        WHERE skill_name = $1 AND version = $2
        LIMIT 1
    `
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest, published bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, skillName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get skill by name and version: %w", err)
	}
	var skillJSON models.SkillJSON
	if err := json.Unmarshal(valueJSON, &skillJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill JSON: %w", err)
	}
	return &models.SkillResponse{
		Skill: skillJSON,
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Published:   published,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetAllVersionsBySkillName(ctx context.Context, tx pgx.Tx, skillName string) ([]*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, published, value
        FROM skills
        WHERE skill_name = $1
        ORDER BY published_at DESC
    `
	rows, err := db.getExecutor(tx).Query(ctx, query, skillName)
	if err != nil {
		return nil, fmt.Errorf("failed to query skill versions: %w", err)
	}
	defer rows.Close()
	var results []*models.SkillResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest, published bool
		var valueJSON []byte
		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &published, &valueJSON); err != nil {
			return nil, fmt.Errorf("failed to scan skill row: %w", err)
		}
		var skillJSON models.SkillJSON
		if err := json.Unmarshal(valueJSON, &skillJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal skill JSON: %w", err)
		}
		results = append(results, &models.SkillResponse{
			Skill: skillJSON,
			Meta: models.SkillResponseMeta{
				Official: &models.SkillRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
					Published:   published,
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating skill rows: %w", err)
	}
	if len(results) == 0 {
		return nil, ErrNotFound
	}
	return results, nil
}

func (db *PostgreSQL) CreateSkill(ctx context.Context, tx pgx.Tx, skillJSON *models.SkillJSON, officialMeta *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if skillJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("skillJSON and officialMeta are required")
	}
	if skillJSON.Name == "" || skillJSON.Version == "" {
		return nil, fmt.Errorf("skill name and version are required")
	}
	valueJSON, err := json.Marshal(skillJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal skill JSON: %w", err)
	}
	insert := `
        INSERT INTO skills (skill_name, version, status, published_at, updated_at, is_latest, value)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `
	if _, err := db.getExecutor(tx).Exec(ctx, insert,
		skillJSON.Name,
		skillJSON.Version,
		officialMeta.Status,
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		officialMeta.IsLatest,
		valueJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to insert skill: %w", err)
	}
	return &models.SkillResponse{
		Skill: *skillJSON,
		Meta: models.SkillResponseMeta{
			Official: officialMeta,
		},
	}, nil
}

func (db *PostgreSQL) UpdateSkill(ctx context.Context, tx pgx.Tx, skillName, version string, skillJSON *models.SkillJSON) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if skillJSON == nil {
		return nil, fmt.Errorf("skillJSON is required")
	}
	if skillJSON.Name != skillName || skillJSON.Version != version {
		return nil, fmt.Errorf("%w: skill name and version in JSON must match parameters", ErrInvalidInput)
	}
	valueJSON, err := json.Marshal(skillJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated skill: %w", err)
	}
	query := `
        UPDATE skills
        SET value = $1, updated_at = NOW()
        WHERE skill_name = $2 AND version = $3
        RETURNING skill_name, version, status, published_at, updated_at, is_latest
    `
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	if err := db.getExecutor(tx).QueryRow(ctx, query, valueJSON, skillName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update skill: %w", err)
	}
	return &models.SkillResponse{
		Skill: *skillJSON,
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (db *PostgreSQL) SetSkillStatus(ctx context.Context, tx pgx.Tx, skillName, version string, status string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	query := `
        UPDATE skills
        SET status = $1, updated_at = NOW()
        WHERE skill_name = $2 AND version = $3
        RETURNING skill_name, version, status, value, published_at, updated_at, is_latest
    `
	var name, vers, currentStatus string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := db.getExecutor(tx).QueryRow(ctx, query, status, skillName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to update skill status: %w", err)
	}
	var skillJSON models.SkillJSON
	if err := json.Unmarshal(valueJSON, &skillJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill JSON: %w", err)
	}
	return &models.SkillResponse{
		Skill: skillJSON,
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{
				Status:      currentStatus,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (db *PostgreSQL) GetCurrentLatestSkillVersion(ctx context.Context, tx pgx.Tx, skillName string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `
        SELECT skill_name, version, status, value, published_at, updated_at, is_latest
        FROM skills
        WHERE skill_name = $1 AND is_latest = true
    `
	row := executor.QueryRow(ctx, query, skillName)
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte
	if err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan skill row: %w", err)
	}
	var skillJSON models.SkillJSON
	if err := json.Unmarshal(jsonValue, &skillJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal skill JSON: %w", err)
	}
	return &models.SkillResponse{
		Skill: skillJSON,
		Meta: models.SkillResponseMeta{
			Official: &models.SkillRegistryExtensions{
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Status:      status,
			},
		},
	}, nil
}

func (db *PostgreSQL) CountSkillVersions(ctx context.Context, tx pgx.Tx, skillName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `SELECT COUNT(*) FROM skills WHERE skill_name = $1`
	var count int
	if err := executor.QueryRow(ctx, query, skillName).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count skill versions: %w", err)
	}
	return count, nil
}

func (db *PostgreSQL) CheckSkillVersionExists(ctx context.Context, tx pgx.Tx, skillName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `SELECT EXISTS(SELECT 1 FROM skills WHERE skill_name = $1 AND version = $2)`
	var exists bool
	if err := executor.QueryRow(ctx, query, skillName, version).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check skill version existence: %w", err)
	}
	return exists, nil
}

func (db *PostgreSQL) UnmarkSkillAsLatest(ctx context.Context, tx pgx.Tx, skillName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	executor := db.getExecutor(tx)
	query := `UPDATE skills SET is_latest = false WHERE skill_name = $1 AND is_latest = true`
	if _, err := executor.Exec(ctx, query, skillName); err != nil {
		return fmt.Errorf("failed to unmark latest skill version: %w", err)
	}
	return nil
}

// PublishSkill marks a skill as published
func (db *PostgreSQL) PublishSkill(ctx context.Context, tx pgx.Tx, skillName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE skills SET published = true, published_date = NOW() WHERE skill_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, skillName, version)
	if err != nil {
		return fmt.Errorf("failed to publish skill: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UnpublishSkill marks a skill as unpublished
func (db *PostgreSQL) UnpublishSkill(ctx context.Context, tx pgx.Tx, skillName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `UPDATE skills SET published = false, unpublished_date = NOW() WHERE skill_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, skillName, version)
	if err != nil {
		return fmt.Errorf("failed to unpublish skill: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// IsSkillPublished checks if a skill is published
func (db *PostgreSQL) IsSkillPublished(ctx context.Context, tx pgx.Tx, skillName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	executor := db.getExecutor(tx)
	query := `SELECT published FROM skills WHERE skill_name = $1 AND version = $2`

	var published bool
	err := executor.QueryRow(ctx, query, skillName, version).Scan(&published)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrNotFound
		}
		return false, fmt.Errorf("failed to check if skill is published: %w", err)
	}

	return published, nil
}

// CreateDeployment creates a new deployment record
func (db *PostgreSQL) CreateDeployment(ctx context.Context, tx pgx.Tx, deployment *models.Deployment) error {
	executor := db.getExecutor(tx)

	configJSON, err := json.Marshal(deployment.Config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		INSERT INTO deployments (server_name, version, status, config, prefer_remote, resource_type)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	// Default to 'mcp' if not specified
	resourceType := deployment.ResourceType
	if resourceType == "" {
		resourceType = "mcp"
	}

	_, err = executor.Exec(ctx, query,
		deployment.ServerName,
		deployment.Version,
		deployment.Status,
		configJSON,
		deployment.PreferRemote,
		resourceType,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrAlreadyExists
		}
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	return nil
}

// GetDeployments retrieves all deployed servers
func (db *PostgreSQL) GetDeployments(ctx context.Context, tx pgx.Tx) ([]*models.Deployment, error) {
	executor := db.getExecutor(tx)

	query := `
		SELECT server_name, version, deployed_at, updated_at, status, config, prefer_remote, resource_type
		FROM deployments
		ORDER BY deployed_at DESC
	`

	rows, err := executor.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployments: %w", err)
	}
	defer rows.Close()

	var deployments []*models.Deployment
	for rows.Next() {
		var d models.Deployment
		var configJSON []byte

		err := rows.Scan(
			&d.ServerName,
			&d.Version,
			&d.DeployedAt,
			&d.UpdatedAt,
			&d.Status,
			&configJSON,
			&d.PreferRemote,
			&d.ResourceType,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan deployment: %w", err)
		}

		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &d.Config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal config: %w", err)
			}
		}
		if d.Config == nil {
			d.Config = make(map[string]string)
		}

		deployments = append(deployments, &d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating deployments: %w", err)
	}

	return deployments, nil
}

// GetDeploymentByName retrieves a specific deployment
func (db *PostgreSQL) GetDeploymentByNameAndVersion(ctx context.Context, tx pgx.Tx, serverName string, version string) (*models.Deployment, error) {
	executor := db.getExecutor(tx)

	query := `
		SELECT server_name, version, deployed_at, updated_at, status, config, prefer_remote, resource_type
		FROM deployments
		WHERE server_name = $1 AND version = $2
	`

	var d models.Deployment
	var configJSON []byte

	err := executor.QueryRow(ctx, query, serverName, version).Scan(
		&d.ServerName,
		&d.Version,
		&d.DeployedAt,
		&d.UpdatedAt,
		&d.Status,
		&configJSON,
		&d.PreferRemote,
		&d.ResourceType,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("failed to get deployment: %w", err)
	}

	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &d.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal config: %w", err)
		}
	}
	if d.Config == nil {
		d.Config = make(map[string]string)
	}

	return &d, nil
}

// UpdateDeploymentConfig updates the configuration for a deployment
func (db *PostgreSQL) UpdateDeploymentConfig(ctx context.Context, tx pgx.Tx, serverName string, config map[string]string) error {
	executor := db.getExecutor(tx)

	configJSON, err := json.Marshal(config)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	query := `
		UPDATE deployments
		SET config = $2
		WHERE server_name = $1
	`

	result, err := executor.Exec(ctx, query, serverName, configJSON)
	if err != nil {
		return fmt.Errorf("failed to update deployment config: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// UpdateDeploymentStatus updates the status of a deployment
func (db *PostgreSQL) UpdateDeploymentStatus(ctx context.Context, tx pgx.Tx, serverName, version string, status string) error {
	executor := db.getExecutor(tx)

	query := `
		UPDATE deployments
		SET status = $3
		WHERE server_name = $1 AND version = $2
	`

	result, err := executor.Exec(ctx, query, serverName, version, status)
	if err != nil {
		return fmt.Errorf("failed to update deployment status: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// RemoveDeployment removes a deployment
func (db *PostgreSQL) RemoveDeployment(ctx context.Context, tx pgx.Tx, serverName string, version string) error {
	executor := db.getExecutor(tx)

	query := `DELETE FROM deployments WHERE server_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to delete deployment: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// DeleteAgent permanently removes an agent version from the database
func (db *PostgreSQL) DeleteAgent(ctx context.Context, tx pgx.Tx, agentName, version string) error {
	executor := db.getExecutor(tx)

	query := `DELETE FROM agents WHERE agent_name = $1 AND version = $2`

	result, err := executor.Exec(ctx, query, agentName, version)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}

	if result.RowsAffected() == 0 {
		return ErrNotFound
	}

	return nil
}

// Close closes the database connection
func (db *PostgreSQL) Close() error {
	db.pool.Close()
	return nil
}

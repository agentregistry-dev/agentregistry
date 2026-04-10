package database

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	dbUtils "github.com/agentregistry-dev/agentregistry/pkg/registry/database/utils"
	apiv0 "github.com/modelcontextprotocol/registry/pkg/api/v0"
	"github.com/modelcontextprotocol/registry/pkg/model"
)

type serverStore struct {
	repositoryBase
	tx pgx.Tx
}

var _ database.ServerStore = (*serverStore)(nil)

func (s *serverStore) ListServers(
	ctx context.Context,
	filter *database.ServerFilter,
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
		semanticLiteral, err = dbUtils.VectorLiteral(filter.Semantic.QueryEmbedding)
		if err != nil {
			return nil, "", fmt.Errorf("invalid semantic embedding: %w", err)
		}
	}

	var whereConditions []string
	args := []any{}
	argIndex := 1

	if filter != nil { //nolint:nestif
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
        SELECT server_name, version, status, published_at, updated_at, is_latest, value`
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

	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query servers: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var serverName, version, status string
		var isLatest bool
		var publishedAt, updatedAt time.Time
		var valueJSON []byte
		var semanticScore sql.NullFloat64

		var scanErr error
		if semanticActive {
			scanErr = rows.Scan(&serverName, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON, &semanticScore)
		} else {
			scanErr = rows.Scan(&serverName, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		}
		if scanErr != nil {
			return nil, "", fmt.Errorf("failed to scan server row: %w", scanErr)
		}

		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal server JSON: %w", err)
		}

		if semanticActive && semanticScore.Valid {
			dbUtils.AnnotateServerSemanticScore(&serverJSON, semanticScore.Float64)
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

func (s *serverStore) GetServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`

	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte

	err := s.executor.QueryRow(ctx, query, serverName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name: %w", err)
	}

	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
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

	return serverResponse, nil
}

func (s *serverStore) GetServerVersion(ctx context.Context, serverName string, version string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1 AND version = $2
		ORDER BY published_at DESC
		LIMIT 1
	`

	var name, vers, status string
	var isLatest bool
	var publishedAt, updatedAt time.Time
	var valueJSON []byte

	err := s.executor.QueryRow(ctx, query, serverName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get server by name and version: %w", err)
	}

	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
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

	return serverResponse, nil
}

func (s *serverStore) GetServerVersions(ctx context.Context, serverName string) ([]*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT server_name, version, status, published_at, updated_at, is_latest, value
		FROM servers
		WHERE server_name = $1
		ORDER BY published_at DESC
	`

	rows, err := s.executor.Query(ctx, query, serverName)
	if err != nil {
		return nil, fmt.Errorf("failed to query server versions: %w", err)
	}
	defer rows.Close()

	var results []*apiv0.ServerResponse
	for rows.Next() {
		var name, version, status string
		var isLatest bool
		var publishedAt, updatedAt time.Time
		var valueJSON []byte

		err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
		if err != nil {
			return nil, fmt.Errorf("failed to scan server row: %w", err)
		}

		var serverJSON apiv0.ServerJSON
		if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
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
		return nil, fmt.Errorf("error iterating rows: %w", err)
	}

	if len(results) == 0 {
		return nil, database.ErrNotFound
	}

	return results, nil
}

func (s *serverStore) CreateServer(ctx context.Context, serverJSON *apiv0.ServerJSON, officialMeta *apiv0.RegistryExtensions) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if serverJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("serverJSON and officialMeta are required")
	}

	if serverJSON.Name == "" || serverJSON.Version == "" {
		return nil, fmt.Errorf("server name and version are required")
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: serverJSON.Name,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal server JSON: %w", err)
	}

	insertQuery := `
		INSERT INTO servers (server_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, err = s.executor.Exec(ctx, insertQuery,
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

	serverResponse := &apiv0.ServerResponse{
		Server: *serverJSON,
		Meta: apiv0.ResponseMeta{
			Official: officialMeta,
		},
	}

	return serverResponse, nil
}

func (s *serverStore) UpdateServer(ctx context.Context, serverName, version string, serverJSON *apiv0.ServerJSON) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	if serverJSON == nil {
		return nil, fmt.Errorf("serverJSON is required")
	}

	if serverJSON.Name != serverName || serverJSON.Version != version {
		return nil, fmt.Errorf("%w: server name and version in JSON must match parameters", database.ErrInvalidInput)
	}

	valueJSON, err := json.Marshal(serverJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal updated server: %w", err)
	}

	query := `
		UPDATE servers
		SET value = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, published_at, updated_at, is_latest
	`

	var name, vers, status string
	var isLatest bool
	var publishedAt, updatedAt time.Time

	err = s.executor.QueryRow(ctx, query, valueJSON, serverName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server: %w", err)
	}

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

func (s *serverStore) SetServerStatus(ctx context.Context, serverName, version string, status string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
		UPDATE servers
		SET status = $1, updated_at = NOW()
		WHERE server_name = $2 AND version = $3
		RETURNING server_name, version, status, value, published_at, updated_at, is_latest
	`

	var name, vers, currentStatus string
	var isLatest bool
	var publishedAt, updatedAt time.Time
	var valueJSON []byte

	err := s.executor.QueryRow(ctx, query, status, serverName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update server status: %w", err)
	}

	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(valueJSON, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

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

func (s *serverStore) GetLatestServer(ctx context.Context, serverName string) (*apiv0.ServerResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT server_name, version, status, value, published_at, updated_at, is_latest
		FROM servers
		WHERE server_name = $1 AND is_latest = true
	`

	row := s.executor.QueryRow(ctx, query, serverName)

	var name, version, status string
	var isLatest bool
	var publishedAt, updatedAt time.Time
	var jsonValue []byte

	err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan server row: %w", err)
	}

	var serverJSON apiv0.ServerJSON
	if err := json.Unmarshal(jsonValue, &serverJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal server JSON: %w", err)
	}

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

func (s *serverStore) CountServerVersions(ctx context.Context, serverName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return 0, err
	}

	query := `SELECT COUNT(*) FROM servers WHERE server_name = $1`

	var count int
	err := s.executor.QueryRow(ctx, query, serverName).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("failed to count server versions: %w", err)
	}

	return count, nil
}

func (s *serverStore) CheckVersionExists(ctx context.Context, serverName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return false, err
	}

	query := `SELECT EXISTS(SELECT 1 FROM servers WHERE server_name = $1 AND version = $2)`

	var exists bool
	err := s.executor.QueryRow(ctx, query, serverName, version).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("failed to check version existence: %w", err)
	}

	return exists, nil
}

func (s *serverStore) UnmarkAsLatest(ctx context.Context, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return err
	}

	query := `UPDATE servers SET is_latest = false WHERE server_name = $1 AND is_latest = true`

	_, err := s.executor.Exec(ctx, query, serverName)
	if err != nil {
		return fmt.Errorf("failed to unmark latest version: %w", err)
	}

	return nil
}

// Acquires a transaction-scoped advisory lock to serialize concurrent creates.
func (s *serverStore) AcquireServerCreateLock(ctx context.Context, serverName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if s.tx == nil {
		return fmt.Errorf("server create lock requires an active transaction")
	}
	lockKey := "server." + serverName
	_, err := s.tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtext($1))", lockKey)
	if err != nil {
		return fmt.Errorf("failed to acquire server create lock: %w", err)
	}
	return nil
}

func (s *serverStore) DeleteServer(ctx context.Context, serverName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionDelete, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return err
	}

	var wasLatest bool
	err := s.executor.QueryRow(ctx,
		`SELECT is_latest FROM servers WHERE server_name = $1 AND version = $2`,
		serverName, version,
	).Scan(&wasLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("failed to check server latest status: %w", err)
	}

	query := `DELETE FROM servers WHERE server_name = $1 AND version = $2`
	result, err := s.executor.Exec(ctx, query, serverName, version)
	if err != nil {
		return fmt.Errorf("failed to delete server: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}

	if wasLatest {
		promoteQuery := `
			UPDATE servers SET is_latest = true
			WHERE server_name = $1
			  AND version = (
			    SELECT version FROM servers
			    WHERE server_name = $1
			    ORDER BY published_at DESC
			    LIMIT 1
			  )
		`
		if _, err := s.executor.Exec(ctx, promoteQuery, serverName); err != nil {
			return fmt.Errorf("failed to promote next latest server version: %w", err)
		}
	}

	return nil
}

func (s *serverStore) SetServerEmbedding(ctx context.Context, serverName, version string, embedding *database.SemanticEmbedding) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return err
	}

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
		vectorLiteral, err := dbUtils.VectorLiteral(embedding.Vector)
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

	result, err := s.executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update server embedding: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Returns metadata only, not the vector payload.
func (s *serverStore) GetServerEmbeddingMetadata(ctx context.Context, serverName, version string) (*database.SemanticEmbeddingMetadata, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

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

	err := s.executor.QueryRow(ctx, query, serverName, version).Scan(
		&hasEmbedding,
		&provider,
		&model,
		&dimensions,
		&checksum,
		&generatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to fetch server embedding metadata: %w", err)
	}

	meta := &database.SemanticEmbeddingMetadata{
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

func (s *serverStore) UpsertServerReadme(ctx context.Context, readme *database.ServerReadme) error {
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

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: readme.ServerName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return err
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

	if _, err := s.executor.Exec(ctx, query,
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

func (s *serverStore) GetServerReadme(ctx context.Context, serverName, version string) (*database.ServerReadme, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT server_name, version, content, content_type, size_bytes, sha256, fetched_at
        FROM server_readmes
        WHERE server_name = $1 AND version = $2
        LIMIT 1
    `

	row := s.executor.QueryRow(ctx, query, serverName, version)
	return scanServerReadme(row)
}

func (s *serverStore) GetLatestServerReadme(ctx context.Context, serverName string) (*database.ServerReadme, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: serverName,
		Type: auth.PermissionArtifactTypeServer,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT sr.server_name, sr.version, sr.content, sr.content_type, sr.size_bytes, sr.sha256, sr.fetched_at
        FROM server_readmes sr
        INNER JOIN servers s ON sr.server_name = s.server_name AND sr.version = s.version
        WHERE sr.server_name = $1 AND s.is_latest = true
        LIMIT 1
    `

	row := s.executor.QueryRow(ctx, query, serverName)
	return scanServerReadme(row)
}

func scanServerReadme(row pgx.Row) (*database.ServerReadme, error) {
	var readme database.ServerReadme
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
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan server readme: %w", err)
	}
	return &readme, nil
}

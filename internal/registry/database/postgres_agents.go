package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
	dbUtils "github.com/agentregistry-dev/agentregistry/pkg/registry/database/utils"
)

type agentStore struct {
	repositoryBase
}

var _ database.AgentStore = (*agentStore)(nil)

func (s *agentStore) ListAgents(ctx context.Context, filter *database.AgentFilter, cursor string, limit int) ([]*models.AgentResponse, string, error) {
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
		SELECT agent_name, version, status, published_at, updated_at, is_latest, value`
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

	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query agents: %w", err)
	}
	defer rows.Close()

	var results []*models.AgentResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte
		var semanticScore sql.NullFloat64

		var scanErr error
		if semanticActive {
			scanErr = rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON, &semanticScore)
		} else {
			scanErr = rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON)
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

func (s *agentStore) GetAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, value
		FROM agents
		WHERE agent_name = $1 AND is_latest = true
		ORDER BY published_at DESC
		LIMIT 1
	`
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := s.executor.QueryRow(ctx, query, agentName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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
			},
		},
	}, nil
}

func (s *agentStore) GetAgentVersion(ctx context.Context, agentName, version string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
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
	if err := s.executor.QueryRow(ctx, query, agentName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *agentStore) GetAgentVersions(ctx context.Context, agentName string) ([]*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT agent_name, version, status, published_at, updated_at, is_latest, value
		FROM agents
		WHERE agent_name = $1
		ORDER BY published_at DESC
	`
	rows, err := s.executor.Query(ctx, query, agentName)
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
		return nil, database.ErrNotFound
	}
	return results, nil
}

func (s *agentStore) CreateAgent(ctx context.Context, agentJSON *models.AgentJSON, officialMeta *models.AgentRegistryExtensions) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if agentJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("agentJSON and officialMeta are required")
	}
	if agentJSON.Name == "" || agentJSON.Version == "" {
		return nil, fmt.Errorf("agent name and version are required")
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: agentJSON.Name,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(agentJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal agent JSON: %w", err)
	}
	insert := `
		INSERT INTO agents (agent_name, version, status, published_at, updated_at, is_latest, value)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	if _, err := s.executor.Exec(ctx, insert,
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

func (s *agentStore) UpdateAgent(ctx context.Context, agentName, version string, agentJSON *models.AgentJSON) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
	}

	if agentJSON == nil {
		return nil, fmt.Errorf("agentJSON is required")
	}
	if agentJSON.Name != agentName || agentJSON.Version != version {
		return nil, fmt.Errorf("%w: agent name and version in JSON must match parameters", database.ErrInvalidInput)
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
	if err := s.executor.QueryRow(ctx, query, valueJSON, agentName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *agentStore) SetAgentStatus(ctx context.Context, agentName, version string, status string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
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
	if err := s.executor.QueryRow(ctx, query, status, agentName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *agentStore) GetLatestAgent(ctx context.Context, agentName string) (*models.AgentResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return nil, err
	}

	query := `
		SELECT agent_name, version, status, value, published_at, updated_at, is_latest
		FROM agents
		WHERE agent_name = $1 AND is_latest = true
	`
	row := s.executor.QueryRow(ctx, query, agentName)
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte
	if err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *agentStore) CountAgentVersions(ctx context.Context, agentName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return 0, err
	}

	query := `SELECT COUNT(*) FROM agents WHERE agent_name = $1`
	var count int
	if err := s.executor.QueryRow(ctx, query, agentName).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count agent versions: %w", err)
	}
	return count, nil
}

func (s *agentStore) CheckAgentVersionExists(ctx context.Context, agentName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return false, err
	}

	query := `SELECT EXISTS(SELECT 1 FROM agents WHERE agent_name = $1 AND version = $2)`
	var exists bool
	if err := s.executor.QueryRow(ctx, query, agentName, version).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check agent version existence: %w", err)
	}
	return exists, nil
}

func (s *agentStore) UnmarkAgentAsLatest(ctx context.Context, agentName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// note: we do a push check because this is called during an artifact's creation operation, which automatically marks the new version as latest.
	// maybe we should add a parameter to the function to indicate if it's from a creation operation or not? this would be important if we allow manual marking of latest.
	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return err
	}

	query := `UPDATE agents SET is_latest = false WHERE agent_name = $1 AND is_latest = true`
	if _, err := s.executor.Exec(ctx, query, agentName); err != nil {
		return fmt.Errorf("failed to unmark latest agent version: %w", err)
	}
	return nil
}

func (s *agentStore) SetAgentEmbedding(ctx context.Context, agentName, version string, embedding *database.SemanticEmbedding) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return err
	}

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
		vectorLiteral, err := dbUtils.VectorLiteral(embedding.Vector)
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

	result, err := s.executor.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to update agent embedding: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

// Returns metadata only, not the vector payload.
func (s *agentStore) GetAgentEmbeddingMetadata(ctx context.Context, agentName, version string) (*database.SemanticEmbeddingMetadata, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
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

	err := s.executor.QueryRow(ctx, query, agentName, version).Scan(
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
		return nil, fmt.Errorf("failed to fetch agent embedding metadata: %w", err)
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

// If the deleted version was latest, the most recently published remaining
// version is promoted.
func (s *agentStore) DeleteAgent(ctx context.Context, agentName, version string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionDelete, auth.Resource{
		Name: agentName,
		Type: auth.PermissionArtifactTypeAgent,
	}); err != nil {
		return err
	}

	// Check if the version being deleted is the current latest.
	var wasLatest bool
	err := s.executor.QueryRow(ctx,
		`SELECT is_latest FROM agents WHERE agent_name = $1 AND version = $2`,
		agentName, version,
	).Scan(&wasLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("failed to check agent latest status: %w", err)
	}

	query := `DELETE FROM agents WHERE agent_name = $1 AND version = $2`
	result, err := s.executor.Exec(ctx, query, agentName, version)
	if err != nil {
		return fmt.Errorf("failed to delete agent: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}

	if wasLatest {
		promoteQuery := `
			UPDATE agents SET is_latest = true
			WHERE agent_name = $1
			  AND version = (
			    SELECT version FROM agents
			    WHERE agent_name = $1
			    ORDER BY published_at DESC
			    LIMIT 1
			  )
		`
		if _, err := s.executor.Exec(ctx, promoteQuery, agentName); err != nil {
			return fmt.Errorf("failed to promote next latest agent version: %w", err)
		}
	}

	return nil
}

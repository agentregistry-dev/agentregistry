package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type promptStore struct {
	repositoryBase
}

var _ database.PromptStore = (*promptStore)(nil)

func (s *promptStore) ListPrompts(ctx context.Context, filter *database.PromptFilter, cursor string, limit int) ([]*models.PromptResponse, string, error) {
	if limit <= 0 {
		limit = 10
	}
	if ctx.Err() != nil {
		return nil, "", ctx.Err()
	}

	var whereConditions []string
	args := []any{}
	argIndex := 1

	if filter != nil { //nolint:nestif
		if filter.Name != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("prompt_name = $%d", argIndex))
			args = append(args, *filter.Name)
			argIndex++
		}
		if filter.UpdatedSince != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("updated_at > $%d", argIndex))
			args = append(args, *filter.UpdatedSince)
			argIndex++
		}
		if filter.SubstringName != nil {
			whereConditions = append(whereConditions, fmt.Sprintf("prompt_name ILIKE $%d", argIndex))
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

	if cursor != "" {
		parts := strings.SplitN(cursor, ":", 2)
		if len(parts) == 2 {
			cursorName := parts[0]
			cursorVersion := parts[1]
			whereConditions = append(whereConditions, fmt.Sprintf("(prompt_name > $%d OR (prompt_name = $%d AND version > $%d))", argIndex, argIndex+1, argIndex+2))
			args = append(args, cursorName, cursorName, cursorVersion)
			argIndex += 3
		} else {
			whereConditions = append(whereConditions, fmt.Sprintf("prompt_name > $%d", argIndex))
			args = append(args, cursor)
			argIndex++
		}
	}

	whereClause := ""
	if len(whereConditions) > 0 {
		whereClause = "WHERE " + strings.Join(whereConditions, " AND ")
	}

	query := fmt.Sprintf(`
        SELECT prompt_name, version, status, published_at, updated_at, is_latest, value
        FROM prompts
        %s
        ORDER BY prompt_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query prompts: %w", err)
	}
	defer rows.Close()

	var results []*models.PromptResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
			return nil, "", fmt.Errorf("failed to scan prompt row: %w", err)
		}

		var promptJSON models.PromptJSON
		if err := json.Unmarshal(valueJSON, &promptJSON); err != nil {
			return nil, "", fmt.Errorf("failed to unmarshal prompt JSON: %w", err)
		}

		resp := &models.PromptResponse{
			Prompt: promptJSON,
			Meta: models.PromptResponseMeta{
				Official: &models.PromptRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		}
		results = append(results, resp)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("error iterating prompt rows: %w", err)
	}

	nextCursor := ""
	if len(results) > 0 && len(results) >= limit {
		last := results[len(results)-1]
		nextCursor = last.Prompt.Name + ":" + last.Prompt.Version
	}
	return results, nextCursor, nil
}

func (s *promptStore) GetPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT prompt_name, version, status, published_at, updated_at, is_latest, value
        FROM prompts
        WHERE prompt_name = $1 AND is_latest = true
        ORDER BY published_at DESC
        LIMIT 1
    `
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := s.executor.QueryRow(ctx, query, promptName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get prompt by name: %w", err)
	}
	var promptJSON models.PromptJSON
	if err := json.Unmarshal(valueJSON, &promptJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt JSON: %w", err)
	}
	return &models.PromptResponse{
		Prompt: promptJSON,
		Meta: models.PromptResponseMeta{
			Official: &models.PromptRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (s *promptStore) GetPromptVersion(ctx context.Context, promptName, version string) (*models.PromptResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT prompt_name, version, status, published_at, updated_at, is_latest, value
        FROM prompts
        WHERE prompt_name = $1 AND version = $2
        LIMIT 1
    `
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := s.executor.QueryRow(ctx, query, promptName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get prompt by name and version: %w", err)
	}
	var promptJSON models.PromptJSON
	if err := json.Unmarshal(valueJSON, &promptJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt JSON: %w", err)
	}
	return &models.PromptResponse{
		Prompt: promptJSON,
		Meta: models.PromptResponseMeta{
			Official: &models.PromptRegistryExtensions{
				Status:      status,
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
			},
		},
	}, nil
}

func (s *promptStore) GetPromptVersions(ctx context.Context, promptName string) ([]*models.PromptResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT prompt_name, version, status, published_at, updated_at, is_latest, value
        FROM prompts
        WHERE prompt_name = $1
        ORDER BY published_at DESC
    `
	rows, err := s.executor.Query(ctx, query, promptName)
	if err != nil {
		return nil, fmt.Errorf("failed to query prompt versions: %w", err)
	}
	defer rows.Close()
	var results []*models.PromptResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte
		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
			return nil, fmt.Errorf("failed to scan prompt row: %w", err)
		}
		var promptJSON models.PromptJSON
		if err := json.Unmarshal(valueJSON, &promptJSON); err != nil {
			return nil, fmt.Errorf("failed to unmarshal prompt JSON: %w", err)
		}
		results = append(results, &models.PromptResponse{
			Prompt: promptJSON,
			Meta: models.PromptResponseMeta{
				Official: &models.PromptRegistryExtensions{
					Status:      status,
					PublishedAt: publishedAt,
					UpdatedAt:   updatedAt,
					IsLatest:    isLatest,
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating prompt rows: %w", err)
	}
	if len(results) == 0 {
		return nil, database.ErrNotFound
	}
	return results, nil
}

func (s *promptStore) CreatePrompt(ctx context.Context, promptJSON *models.PromptJSON, officialMeta *models.PromptRegistryExtensions) (*models.PromptResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if promptJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("promptJSON and officialMeta are required")
	}
	if promptJSON.Name == "" || promptJSON.Version == "" {
		return nil, fmt.Errorf("prompt name and version are required")
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: promptJSON.Name,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(promptJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal prompt JSON: %w", err)
	}
	insert := `
        INSERT INTO prompts (prompt_name, version, status, published_at, updated_at, is_latest, value)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `
	if _, err := s.executor.Exec(ctx, insert,
		promptJSON.Name,
		promptJSON.Version,
		officialMeta.Status,
		officialMeta.PublishedAt,
		officialMeta.UpdatedAt,
		officialMeta.IsLatest,
		valueJSON,
	); err != nil {
		return nil, fmt.Errorf("failed to insert prompt: %w", err)
	}
	return &models.PromptResponse{
		Prompt: *promptJSON,
		Meta: models.PromptResponseMeta{
			Official: officialMeta,
		},
	}, nil
}

func (s *promptStore) GetLatestPrompt(ctx context.Context, promptName string) (*models.PromptResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT prompt_name, version, status, value, published_at, updated_at, is_latest
        FROM prompts
        WHERE prompt_name = $1 AND is_latest = true
    `
	row := s.executor.QueryRow(ctx, query, promptName)
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte
	if err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to scan prompt row: %w", err)
	}
	var promptJSON models.PromptJSON
	if err := json.Unmarshal(jsonValue, &promptJSON); err != nil {
		return nil, fmt.Errorf("failed to unmarshal prompt JSON: %w", err)
	}
	return &models.PromptResponse{
		Prompt: promptJSON,
		Meta: models.PromptResponseMeta{
			Official: &models.PromptRegistryExtensions{
				PublishedAt: publishedAt,
				UpdatedAt:   updatedAt,
				IsLatest:    isLatest,
				Status:      status,
			},
		},
	}, nil
}

func (s *promptStore) CountPromptVersions(ctx context.Context, promptName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return 0, err
	}

	query := `SELECT COUNT(*) FROM prompts WHERE prompt_name = $1`
	var count int
	if err := s.executor.QueryRow(ctx, query, promptName).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count prompt versions: %w", err)
	}
	return count, nil
}

func (s *promptStore) CheckPromptVersionExists(ctx context.Context, promptName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return false, err
	}

	query := `SELECT EXISTS(SELECT 1 FROM prompts WHERE prompt_name = $1 AND version = $2)`
	var exists bool
	if err := s.executor.QueryRow(ctx, query, promptName, version).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check prompt version existence: %w", err)
	}
	return exists, nil
}

func (s *promptStore) UnmarkPromptAsLatest(ctx context.Context, promptName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return err
	}

	query := `UPDATE prompts SET is_latest = false WHERE prompt_name = $1 AND is_latest = true`
	if _, err := s.executor.Exec(ctx, query, promptName); err != nil {
		return fmt.Errorf("failed to unmark latest prompt version: %w", err)
	}
	return nil
}

func (s *promptStore) DeletePrompt(ctx context.Context, promptName, version string) error {
	if err := s.authz.Check(ctx, auth.PermissionActionDelete, auth.Resource{
		Name: promptName,
		Type: auth.PermissionArtifactTypePrompt,
	}); err != nil {
		return err
	}

	// Check if the version being deleted is the current latest.
	var wasLatest bool
	err := s.executor.QueryRow(ctx,
		`SELECT is_latest FROM prompts WHERE prompt_name = $1 AND version = $2`,
		promptName, version,
	).Scan(&wasLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("failed to check prompt latest status: %w", err)
	}

	// Delete the requested version.
	query := `DELETE FROM prompts WHERE prompt_name = $1 AND version = $2`
	result, err := s.executor.Exec(ctx, query, promptName, version)
	if err != nil {
		return fmt.Errorf("failed to delete prompt: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}

	// If the deleted version was latest, promote the most recently published
	// remaining version so that GetPrompt keeps working.
	if wasLatest {
		promoteQuery := `
			UPDATE prompts SET is_latest = true
			WHERE prompt_name = $1
			  AND version = (
			    SELECT version FROM prompts
			    WHERE prompt_name = $1
			    ORDER BY published_at DESC
			    LIMIT 1
			  )
		`
		if _, err := s.executor.Exec(ctx, promoteQuery, promptName); err != nil {
			return fmt.Errorf("failed to promote next latest prompt version: %w", err)
		}
	}

	return nil
}

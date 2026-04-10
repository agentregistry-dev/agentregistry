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

type skillStore struct {
	repositoryBase
}

var _ database.SkillStore = (*skillStore)(nil)

func (s *skillStore) ListSkills(ctx context.Context, filter *database.SkillFilter, cursor string, limit int) ([]*models.SkillResponse, string, error) {
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
        SELECT skill_name, version, status, published_at, updated_at, is_latest, value
        FROM skills
        %s
        ORDER BY skill_name, version
        LIMIT $%d
    `, whereClause, argIndex)
	args = append(args, limit)

	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, "", fmt.Errorf("failed to query skills: %w", err)
	}
	defer rows.Close()

	var results []*models.SkillResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte

		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
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

func (s *skillStore) GetSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, value
        FROM skills
        WHERE skill_name = $1 AND is_latest = true
        ORDER BY published_at DESC
        LIMIT 1
    `
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := s.executor.QueryRow(ctx, query, skillName).Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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
			},
		},
	}, nil
}

func (s *skillStore) GetSkillVersion(ctx context.Context, skillName, version string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, value
        FROM skills
        WHERE skill_name = $1 AND version = $2
        LIMIT 1
    `
	var name, vers, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var valueJSON []byte
	if err := s.executor.QueryRow(ctx, query, skillName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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
			},
		},
	}, nil
}

func (s *skillStore) GetSkillVersions(ctx context.Context, skillName string) ([]*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT skill_name, version, status, published_at, updated_at, is_latest, value
        FROM skills
        WHERE skill_name = $1
        ORDER BY published_at DESC
    `
	rows, err := s.executor.Query(ctx, query, skillName)
	if err != nil {
		return nil, fmt.Errorf("failed to query skill versions: %w", err)
	}
	defer rows.Close()
	var results []*models.SkillResponse
	for rows.Next() {
		var name, version, status string
		var publishedAt, updatedAt time.Time
		var isLatest bool
		var valueJSON []byte
		if err := rows.Scan(&name, &version, &status, &publishedAt, &updatedAt, &isLatest, &valueJSON); err != nil {
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
				},
			},
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating skill rows: %w", err)
	}
	if len(results) == 0 {
		return nil, database.ErrNotFound
	}
	return results, nil
}

func (s *skillStore) CreateSkill(ctx context.Context, skillJSON *models.SkillJSON, officialMeta *models.SkillRegistryExtensions) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if skillJSON == nil || officialMeta == nil {
		return nil, fmt.Errorf("skillJSON and officialMeta are required")
	}
	if skillJSON.Name == "" || skillJSON.Version == "" {
		return nil, fmt.Errorf("skill name and version are required")
	}

	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: skillJSON.Name,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}
	valueJSON, err := json.Marshal(skillJSON)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal skill JSON: %w", err)
	}
	insert := `
        INSERT INTO skills (skill_name, version, status, published_at, updated_at, is_latest, value)
        VALUES ($1, $2, $3, $4, $5, $6, $7)
    `
	if _, err := s.executor.Exec(ctx, insert,
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

func (s *skillStore) UpdateSkill(ctx context.Context, skillName, version string, skillJSON *models.SkillJSON) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}

	if skillJSON == nil {
		return nil, fmt.Errorf("skillJSON is required")
	}
	if skillJSON.Name != skillName || skillJSON.Version != version {
		return nil, fmt.Errorf("%w: skill name and version in JSON must match parameters", database.ErrInvalidInput)
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
	if err := s.executor.QueryRow(ctx, query, valueJSON, skillName, version).Scan(&name, &vers, &status, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *skillStore) SetSkillStatus(ctx context.Context, skillName, version string, status string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
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
	if err := s.executor.QueryRow(ctx, query, status, skillName, version).Scan(&name, &vers, &currentStatus, &valueJSON, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *skillStore) GetLatestSkill(ctx context.Context, skillName string) (*models.SkillResponse, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return nil, err
	}

	query := `
        SELECT skill_name, version, status, value, published_at, updated_at, is_latest
        FROM skills
        WHERE skill_name = $1 AND is_latest = true
    `
	row := s.executor.QueryRow(ctx, query, skillName)
	var name, version, status string
	var publishedAt, updatedAt time.Time
	var isLatest bool
	var jsonValue []byte
	if err := row.Scan(&name, &version, &status, &jsonValue, &publishedAt, &updatedAt, &isLatest); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
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

func (s *skillStore) CountSkillVersions(ctx context.Context, skillName string) (int, error) {
	if ctx.Err() != nil {
		return 0, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return 0, err
	}

	query := `SELECT COUNT(*) FROM skills WHERE skill_name = $1`
	var count int
	if err := s.executor.QueryRow(ctx, query, skillName).Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to count skill versions: %w", err)
	}
	return count, nil
}

func (s *skillStore) CheckSkillVersionExists(ctx context.Context, skillName, version string) (bool, error) {
	if ctx.Err() != nil {
		return false, ctx.Err()
	}

	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return false, err
	}

	query := `SELECT EXISTS(SELECT 1 FROM skills WHERE skill_name = $1 AND version = $2)`
	var exists bool
	if err := s.executor.QueryRow(ctx, query, skillName, version).Scan(&exists); err != nil {
		return false, fmt.Errorf("failed to check skill version existence: %w", err)
	}
	return exists, nil
}

func (s *skillStore) UnmarkSkillAsLatest(ctx context.Context, skillName string) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}

	// note: we do a push check because this is called during an artifact's creation operation, which automatically marks the new version as latest.
	// maybe we should add a parameter to the function to indicate if it's from a creation operation or not? this would be important if we allow manual marking of latest.
	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return err
	}

	query := `UPDATE skills SET is_latest = false WHERE skill_name = $1 AND is_latest = true`
	if _, err := s.executor.Exec(ctx, query, skillName); err != nil {
		return fmt.Errorf("failed to unmark latest skill version: %w", err)
	}
	return nil
}

func (s *skillStore) DeleteSkill(ctx context.Context, skillName, version string) error {
	if err := s.authz.Check(ctx, auth.PermissionActionDelete, auth.Resource{
		Name: skillName,
		Type: auth.PermissionArtifactTypeSkill,
	}); err != nil {
		return err
	}

	// Check if the version being deleted is the current latest.
	var wasLatest bool
	err := s.executor.QueryRow(ctx,
		`SELECT is_latest FROM skills WHERE skill_name = $1 AND version = $2`,
		skillName, version,
	).Scan(&wasLatest)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return database.ErrNotFound
		}
		return fmt.Errorf("failed to check skill latest status: %w", err)
	}

	query := `DELETE FROM skills WHERE skill_name = $1 AND version = $2`
	result, err := s.executor.Exec(ctx, query, skillName, version)
	if err != nil {
		return fmt.Errorf("failed to delete skill: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}

	if wasLatest {
		promoteQuery := `
			UPDATE skills SET is_latest = true
			WHERE skill_name = $1
			  AND version = (
			    SELECT version FROM skills
			    WHERE skill_name = $1
			    ORDER BY published_at DESC
			    LIMIT 1
			  )
		`
		if _, err := s.executor.Exec(ctx, promoteQuery, skillName); err != nil {
			return fmt.Errorf("failed to promote next latest skill version: %w", err)
		}
	}

	return nil
}

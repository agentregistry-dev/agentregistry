package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/agentregistry-dev/agentregistry/pkg/models"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/auth"
	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

type providerStore struct {
	repositoryBase
}

var _ database.ProviderStore = (*providerStore)(nil)

func (s *providerStore) CreateProvider(ctx context.Context, in *models.CreateProviderInput) (*models.Provider, error) {
	if in == nil {
		return nil, database.ErrInvalidInput
	}
	if strings.TrimSpace(in.ID) == "" || strings.TrimSpace(in.Name) == "" || strings.TrimSpace(in.Platform) == "" {
		return nil, database.ErrInvalidInput
	}
	if err := s.authz.Check(ctx, auth.PermissionActionPublish, auth.Resource{
		Name: in.ID,
		Type: auth.PermissionArtifactTypeProvider,
	}); err != nil {
		return nil, err
	}
	configJSON, err := json.Marshal(in.Config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider config: %w", err)
	}
	query := `
		INSERT INTO providers (id, name, platform, config)
		VALUES ($1, $2, $3, $4)
		RETURNING id, name, platform, COALESCE(config, '{}'::jsonb), created_at, updated_at
	`
	var provider models.Provider
	var configOut []byte
	err = s.executor.QueryRow(ctx, query, in.ID, in.Name, in.Platform, configJSON).Scan(
		&provider.ID,
		&provider.Name,
		&provider.Platform,
		&configOut,
		&provider.CreatedAt,
		&provider.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, database.ErrAlreadyExists
		}
		return nil, fmt.Errorf("failed to create provider: %w", err)
	}
	if len(configOut) > 0 {
		if err := json.Unmarshal(configOut, &provider.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
		}
	}
	if provider.Config == nil {
		provider.Config = map[string]any{}
	}
	return &provider, nil
}

func (s *providerStore) ListProviders(ctx context.Context, platform *string) ([]*models.Provider, error) {
	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Type: auth.PermissionArtifactTypeProvider,
	}); err != nil {
		return nil, err
	}
	query := `SELECT id, name, platform, COALESCE(config, '{}'::jsonb), created_at, updated_at FROM providers`
	args := []any{}
	if platform != nil && strings.TrimSpace(*platform) != "" {
		query += ` WHERE platform = $1`
		args = append(args, strings.TrimSpace(*platform))
	}
	query += ` ORDER BY created_at ASC`
	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list providers: %w", err)
	}
	defer rows.Close()
	var out []*models.Provider
	for rows.Next() {
		var p models.Provider
		var configJSON []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Platform, &configJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan provider: %w", err)
		}
		if len(configJSON) > 0 {
			if err := json.Unmarshal(configJSON, &p.Config); err != nil {
				return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
			}
		}
		if p.Config == nil {
			p.Config = map[string]any{}
		}
		out = append(out, &p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate providers: %w", err)
	}
	return out, nil
}

func (s *providerStore) GetProvider(ctx context.Context, providerID string) (*models.Provider, error) {
	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: providerID,
		Type: auth.PermissionArtifactTypeProvider,
	}); err != nil {
		return nil, err
	}
	query := `SELECT id, name, platform, COALESCE(config, '{}'::jsonb), created_at, updated_at FROM providers WHERE id = $1`
	var p models.Provider
	var configJSON []byte
	if err := s.executor.QueryRow(ctx, query, providerID).Scan(&p.ID, &p.Name, &p.Platform, &configJSON, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get provider: %w", err)
	}
	if len(configJSON) > 0 {
		if err := json.Unmarshal(configJSON, &p.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
		}
	}
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	return &p, nil
}

func (s *providerStore) UpdateProvider(ctx context.Context, providerID string, in *models.UpdateProviderInput) (*models.Provider, error) {
	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: providerID,
		Type: auth.PermissionArtifactTypeProvider,
	}); err != nil {
		return nil, err
	}
	if in == nil {
		return s.GetProvider(ctx, providerID)
	}
	current, err := s.GetProvider(ctx, providerID)
	if err != nil {
		return nil, err
	}
	name := current.Name
	if in.Name != nil {
		name = *in.Name
	}
	config := current.Config
	if in.Config != nil {
		config = in.Config
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal provider config: %w", err)
	}
	query := `
		UPDATE providers
		SET name = $2, config = $3, updated_at = NOW()
		WHERE id = $1
		RETURNING id, name, platform, COALESCE(config, '{}'::jsonb), created_at, updated_at
	`
	var p models.Provider
	var configOut []byte
	if err := s.executor.QueryRow(ctx, query, providerID, name, configJSON).Scan(&p.ID, &p.Name, &p.Platform, &configOut, &p.CreatedAt, &p.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to update provider: %w", err)
	}
	if len(configOut) > 0 {
		if err := json.Unmarshal(configOut, &p.Config); err != nil {
			return nil, fmt.Errorf("failed to unmarshal provider config: %w", err)
		}
	}
	if p.Config == nil {
		p.Config = map[string]any{}
	}
	return &p, nil
}

func (s *providerStore) DeleteProvider(ctx context.Context, providerID string) error {
	if err := s.authz.Check(ctx, auth.PermissionActionDelete, auth.Resource{
		Name: providerID,
		Type: auth.PermissionArtifactTypeProvider,
	}); err != nil {
		return err
	}
	result, err := s.executor.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
	if err != nil {
		return fmt.Errorf("failed to delete provider: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

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

type deploymentStore struct {
	repositoryBase
}

var _ database.DeploymentStore = (*deploymentStore)(nil)

func (s *deploymentStore) CreateDeployment(ctx context.Context, deployment *models.Deployment) error {
	artifactType := auth.PermissionArtifactTypeServer
	if deployment.ResourceType == "agent" {
		artifactType = auth.PermissionArtifactTypeAgent
	}
	if err := s.authz.Check(ctx, auth.PermissionActionDeploy, auth.Resource{
		Name: deployment.ServerName,
		Type: artifactType,
	}); err != nil {
		return err
	}

	envJSON, err := json.Marshal(deployment.Env)
	if err != nil {
		return fmt.Errorf("failed to marshal deployment env: %w", err)
	}

	providerConfigJSON, err := json.Marshal(deployment.ProviderConfig)
	if err != nil {
		return fmt.Errorf("failed to marshal provider config: %w", err)
	}
	providerMetadataJSON, err := json.Marshal(deployment.ProviderMetadata)
	if err != nil {
		return fmt.Errorf("failed to marshal provider metadata: %w", err)
	}

	// Default to 'mcp' if not specified
	resourceType := deployment.ResourceType
	if resourceType == "" {
		resourceType = "mcp"
	}
	providerID := strings.TrimSpace(deployment.ProviderID)
	if providerID == "" {
		return fmt.Errorf("%w: provider id is required", database.ErrInvalidInput)
	}
	origin := deployment.Origin
	if origin == "" {
		origin = "managed"
	}
	deployment.ProviderID = providerID

	if deployment.ID == "" {
		_ = s.executor.QueryRow(ctx, "SELECT uuid_generate_v4()::text").Scan(&deployment.ID)
	}

	query := `
		INSERT INTO deployments (
			id, server_name, version, status, config, prefer_remote, resource_type,
			origin, provider_id, provider_config, provider_metadata, error
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, ''), $10, $11, $12)
	`

	_, err = s.executor.Exec(ctx, query,
		deployment.ID,
		deployment.ServerName,
		deployment.Version,
		deployment.Status,
		envJSON,
		deployment.PreferRemote,
		resourceType,
		origin,
		providerID,
		providerConfigJSON,
		providerMetadataJSON,
		deployment.Error,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return database.ErrAlreadyExists
		}
		return fmt.Errorf("failed to create deployment: %w", err)
	}

	return nil
}

func (s *deploymentStore) ListDeployments(ctx context.Context, filter *models.DeploymentFilter) ([]*models.Deployment, error) {
	where, args, needsProviderJoin := buildDeploymentFilters(filter)

	query := `SELECT
			d.id, d.server_name, d.version, d.deployed_at, d.updated_at, d.status, d.config, d.prefer_remote, d.resource_type,
			d.origin, COALESCE(d.provider_id, ''), COALESCE(d.provider_config, '{}'::jsonb), COALESCE(d.provider_metadata, '{}'::jsonb), COALESCE(d.error, '')
		FROM deployments d`
	if needsProviderJoin {
		query += ` LEFT JOIN providers p ON p.id = d.provider_id`
	}
	if len(where) > 0 {
		query += " WHERE " + strings.Join(where, " AND ")
	}
	query += " ORDER BY d.deployed_at DESC"

	rows, err := s.executor.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to query deployments: %w", err)
	}
	defer rows.Close()

	var deployments []*models.Deployment
	for rows.Next() {
		var d models.Deployment
		var envJSON []byte
		var providerConfigJSON []byte
		var providerMetadataJSON []byte

		err := rows.Scan(
			&d.ID,
			&d.ServerName,
			&d.Version,
			&d.DeployedAt,
			&d.UpdatedAt,
			&d.Status,
			&envJSON,
			&d.PreferRemote,
			&d.ResourceType,
			&d.Origin,
			&d.ProviderID,
			&providerConfigJSON,
			&providerMetadataJSON,
			&d.Error,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan deployment: %w", err)
		}

		if len(envJSON) > 0 {
			if err := json.Unmarshal(envJSON, &d.Env); err != nil {
				return nil, fmt.Errorf("failed to unmarshal deployment env: %w", err)
			}
		}
		if d.Env == nil {
			d.Env = make(map[string]string)
		}
		if err := json.Unmarshal(providerConfigJSON, &d.ProviderConfig); err != nil {
			return nil, fmt.Errorf("failed to scan provider config: %w", err)
		}
		if err := json.Unmarshal(providerMetadataJSON, &d.ProviderMetadata); err != nil {
			return nil, fmt.Errorf("failed to scan provider metadata: %w", err)
		}

		deployments = append(deployments, &d)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating deployments: %w", err)
	}

	return deployments, nil
}

func buildDeploymentFilters(filter *models.DeploymentFilter) ([]string, []any, bool) {
	where := make([]string, 0)
	args := make([]any, 0)
	nextArg := 1
	needsProviderJoin := false
	if filter == nil {
		return where, args, needsProviderJoin
	}

	if filter.Platform != nil {
		platform := strings.ToLower(strings.TrimSpace(*filter.Platform))
		needsProviderJoin = true
		where = append(where, fmt.Sprintf("p.platform = $%d", nextArg))
		args = append(args, platform)
		nextArg++
	}
	if filter.ResourceType != nil {
		where = append(where, fmt.Sprintf("resource_type = $%d", nextArg))
		args = append(args, *filter.ResourceType)
		nextArg++
	}
	if filter.Status != nil {
		where = append(where, fmt.Sprintf("status = $%d", nextArg))
		args = append(args, *filter.Status)
		nextArg++
	}
	if filter.Origin != nil {
		where = append(where, fmt.Sprintf("origin = $%d", nextArg))
		args = append(args, *filter.Origin)
		nextArg++
	}
	if filter.ResourceName != nil {
		where = append(where, fmt.Sprintf("server_name ILIKE $%d", nextArg))
		args = append(args, "%"+*filter.ResourceName+"%")
		nextArg++
	}
	if filter.ProviderID != nil {
		where = append(where, fmt.Sprintf("d.provider_id = $%d", nextArg))
		args = append(args, *filter.ProviderID)
	}

	return where, args, needsProviderJoin
}

func (s *deploymentStore) GetDeployment(ctx context.Context, id string) (*models.Deployment, error) {
	query := `SELECT
			id, server_name, version, deployed_at, updated_at, status, config, prefer_remote, resource_type,
			origin, COALESCE(provider_id, ''), COALESCE(provider_config, '{}'::jsonb), COALESCE(provider_metadata, '{}'::jsonb), COALESCE(error, '')
		FROM deployments
		WHERE id = $1`

	var d models.Deployment
	var envJSON []byte
	var providerConfigJSON []byte
	var providerMetadataJSON []byte
	err := s.executor.QueryRow(ctx, query, id).Scan(
		&d.ID,
		&d.ServerName,
		&d.Version,
		&d.DeployedAt,
		&d.UpdatedAt,
		&d.Status,
		&envJSON,
		&d.PreferRemote,
		&d.ResourceType,
		&d.Origin,
		&d.ProviderID,
		&providerConfigJSON,
		&providerMetadataJSON,
		&d.Error,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, database.ErrNotFound
		}
		return nil, fmt.Errorf("failed to get deployment by id: %w", err)
	}
	if len(envJSON) > 0 {
		if err := json.Unmarshal(envJSON, &d.Env); err != nil {
			return nil, fmt.Errorf("failed to unmarshal deployment env: %w", err)
		}
	}
	if d.Env == nil {
		d.Env = make(map[string]string)
	}
	if err := json.Unmarshal(providerConfigJSON, &d.ProviderConfig); err != nil {
		return nil, fmt.Errorf("failed to scan provider config: %w", err)
	}
	if err := json.Unmarshal(providerMetadataJSON, &d.ProviderMetadata); err != nil {
		return nil, fmt.Errorf("failed to scan provider metadata: %w", err)
	}
	artifactType := auth.PermissionArtifactTypeServer
	if d.ResourceType == "agent" {
		artifactType = auth.PermissionArtifactTypeAgent
	}
	if err := s.authz.Check(ctx, auth.PermissionActionRead, auth.Resource{
		Name: d.ServerName,
		Type: artifactType,
	}); err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *deploymentStore) UpdateDeploymentState(ctx context.Context, id string, patch *models.DeploymentStatePatch) error {
	if patch == nil {
		return fmt.Errorf("%w: deployment state patch is required", database.ErrInvalidInput)
	}

	deployment, err := s.GetDeployment(ctx, id)
	if err != nil {
		return err
	}
	artifactType := auth.PermissionArtifactTypeServer
	if deployment.ResourceType == "agent" {
		artifactType = auth.PermissionArtifactTypeAgent
	}
	if err := s.authz.Check(ctx, auth.PermissionActionEdit, auth.Resource{
		Name: deployment.ServerName,
		Type: artifactType,
	}); err != nil {
		return err
	}

	setStatus := patch.Status != nil
	statusValue := deployment.Status
	if patch.Status != nil {
		statusValue = *patch.Status
	}

	setError := patch.Error != nil
	errorValue := deployment.Error
	if patch.Error != nil {
		errorValue = *patch.Error
	}

	setProviderConfig := patch.ProviderConfig != nil
	providerConfigJSON := []byte("{}")
	if patch.ProviderConfig != nil {
		providerConfigJSON, err = json.Marshal(*patch.ProviderConfig)
		if err != nil {
			return fmt.Errorf("failed to marshal provider config patch: %w", err)
		}
	}

	setProviderMetadata := patch.ProviderMetadata != nil
	providerMetadataJSON := []byte("{}")
	if patch.ProviderMetadata != nil {
		providerMetadataJSON, err = json.Marshal(*patch.ProviderMetadata)
		if err != nil {
			return fmt.Errorf("failed to marshal provider metadata patch: %w", err)
		}
	}

	query := `
		UPDATE deployments
		SET
			status = CASE WHEN $2 THEN $3 ELSE status END,
			error = CASE WHEN $4 THEN $5 ELSE error END,
			provider_config = CASE WHEN $6 THEN $7::jsonb ELSE provider_config END,
			provider_metadata = CASE WHEN $8 THEN $9::jsonb ELSE provider_metadata END,
			updated_at = NOW()
		WHERE id = $1
	`

	result, err := s.executor.Exec(
		ctx,
		query,
		id,
		setStatus,
		statusValue,
		setError,
		errorValue,
		setProviderConfig,
		string(providerConfigJSON),
		setProviderMetadata,
		string(providerMetadataJSON),
	)
	if err != nil {
		return fmt.Errorf("failed to update deployment state: %w", err)
	}

	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}

	return nil
}

func (s *deploymentStore) DeleteDeployment(ctx context.Context, id string) error {
	deployment, err := s.GetDeployment(ctx, id)
	if err != nil {
		return err
	}
	artifactType := auth.PermissionArtifactTypeServer
	if deployment.ResourceType == "agent" {
		artifactType = auth.PermissionArtifactTypeAgent
	}
	if err := s.authz.Check(ctx, auth.PermissionActionDeploy, auth.Resource{
		Name: deployment.ServerName,
		Type: artifactType,
	}); err != nil {
		return err
	}

	query := `DELETE FROM deployments WHERE id = $1`

	result, err := s.executor.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete deployment by id: %w", err)
	}
	if result.RowsAffected() == 0 {
		return database.ErrNotFound
	}
	return nil
}

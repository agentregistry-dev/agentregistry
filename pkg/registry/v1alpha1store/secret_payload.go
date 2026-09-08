package v1alpha1store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	pkgdb "github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

// SecretPayloadStore persists ciphertext separately from Secret metadata.
type SecretPayloadStore struct {
	pool      *pgxpool.Pool
	qualified string
}

// NewSecretPayloadStore binds ciphertext persistence to a configurable table.
func NewSecretPayloadStore(pool *pgxpool.Pool, schema pkgdb.Schema, table string) *SecretPayloadStore {
	return &SecretPayloadStore{pool: pool, qualified: schema.Qualify(table)}
}

func (s *SecretPayloadStore) UpsertSecretPayload(ctx context.Context, namespace, name string, ciphertext []byte) error {
	query := fmt.Sprintf(`INSERT INTO %s (namespace, name, ciphertext)
		VALUES ($1, $2, $3) ON CONFLICT (namespace, name)
		DO UPDATE SET ciphertext = EXCLUDED.ciphertext, updated_at = NOW()`, s.qualified)
	if _, err := s.pool.Exec(ctx, query, namespace, name, ciphertext); err != nil {
		return fmt.Errorf("upsert secret payload: %w", err)
	}
	return nil
}

func (s *SecretPayloadStore) GetSecretPayload(ctx context.Context, namespace, name string) ([]byte, error) {
	query := fmt.Sprintf(`SELECT ciphertext FROM %s WHERE namespace = $1 AND name = $2`, s.qualified)
	var ciphertext []byte
	if err := s.pool.QueryRow(ctx, query, namespace, name).Scan(&ciphertext); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, pkgdb.ErrNotFound
		}
		return nil, fmt.Errorf("get secret payload: %w", err)
	}
	return ciphertext, nil
}

func (s *SecretPayloadStore) DeleteSecretPayload(ctx context.Context, namespace, name string) error {
	query := fmt.Sprintf(`DELETE FROM %s WHERE namespace = $1 AND name = $2`, s.qualified)
	if _, err := s.pool.Exec(ctx, query, namespace, name); err != nil {
		return fmt.Errorf("delete secret payload: %w", err)
	}
	return nil
}

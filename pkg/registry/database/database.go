// Package database exposes the minimum types the agentregistry server
// contract guarantees across builds: the sentinel errors every layer
// returns when input is malformed, a row is missing, etc., and the thin
// Store interface that AppOptions.DatabaseFactory wraps.
//
// Production code reads and writes v1alpha1 envelopes via the generic
// v1alpha1store.Store against the v1alpha1.* PostgreSQL schema.
package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Common database errors surfaced by both the v1alpha1 generic Store
// and any enterprise DatabaseFactory that wraps it.
var (
	ErrNotFound      = errors.New("record not found")
	ErrForbidden     = errors.New("forbidden")
	ErrAlreadyExists = errors.New("record already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrDatabase      = errors.New("database error")
	// ErrDuplicateVersion is returned when an upsert would publish a
	// version that already exists (e.g. a re-publish without bumping).
	// Distinct from v1alpha1.ErrInvalidVersion which covers structural
	// version-string validation (length, literal "latest", format).
	ErrDuplicateVersion = errors.New("version already published")
)

// Store is the root persistence contract AppOptions.DatabaseFactory
// wraps. The OSS implementation (internal/registry/database.PostgreSQL)
// is a pgxpool-backed Store; enterprise builds layer authz / caching /
// secondary indices on top by wrapping a base Store.
//
// The contract is intentionally thin: v1alpha1 consumers reach through
// Pool() to construct their own generic Stores via
// internal/registry/v1alpha1store.NewStores. Backends without a real
// PostgreSQL connection return nil from Pool(); callers must gate any
// pgx-specific functionality accordingly. Close() releases any pooled
// resources on shutdown.
type Store interface {
	Pool() *pgxpool.Pool
	Close() error
}

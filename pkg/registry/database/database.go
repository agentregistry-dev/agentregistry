// Package database exposes the minimum types the agentregistry server
// contract guarantees across builds: the sentinel errors every layer
// returns when input is malformed, a row is missing, etc., and the thin
// Store interface that AppOptions.DatabaseFactory wraps.
//
// Everything kind-specific (per-table Store interfaces, filters, readme
// blobs, embedding metadata) was retired alongside the legacy public.*
// schema — production code reads and writes v1alpha1 envelopes via the
// generic internal/registry/database.Store against the v1alpha1.*
// schema.
package database

import (
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Common database errors surfaced by both the v1alpha1 generic Store
// and any enterprise DatabaseFactory that wraps it.
var (
	ErrNotFound           = errors.New("record not found")
	ErrForbidden          = errors.New("forbidden")
	ErrAlreadyExists      = errors.New("record already exists")
	ErrInvalidInput       = errors.New("invalid input")
	ErrDatabase           = errors.New("database error")
	ErrInvalidVersion     = errors.New("invalid version: cannot publish duplicate version")
	ErrMaxVersionsReached = errors.New("maximum number of versions reached (10000): please reach out at https://github.com/modelcontextprotocol/registry to explain your use case")
)

// Store is the root persistence contract AppOptions.DatabaseFactory
// wraps. The OSS implementation (internal/registry/database.PostgreSQL)
// is a pgxpool-backed Store; enterprise builds layer authz / caching /
// secondary indices on top by wrapping a base Store.
//
// The contract is intentionally thin: v1alpha1 consumers reach through
// Pool() to construct their own generic Stores via
// internal/registry/database.NewV1Alpha1Stores. Backends without a real
// PostgreSQL connection return nil from Pool(); callers must gate any
// pgx-specific functionality accordingly. Close() releases any pooled
// resources on shutdown.
type Store interface {
	Pool() *pgxpool.Pool
	Close() error
}

// ErrStoreNotConfigured is returned by helpers that accept a nil Store.
var ErrStoreNotConfigured = errors.New("store is not configured")

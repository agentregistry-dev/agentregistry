// Package database provides the Database interface for registry operations.
// This package re-exports the internal database interface to allow external
// implementations to wrap and extend the database layer.
package database

import (
	"context"

	internaldatabase "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/jackc/pgx/v5"
)

// Database is the interface for registry database operations.
type Database = internaldatabase.Database

// Filter types for list operations
type (
	ServerFilter = internaldatabase.ServerFilter
	AgentFilter  = internaldatabase.AgentFilter
	SkillFilter  = internaldatabase.SkillFilter
	ServerReadme = internaldatabase.ServerReadme
)

// Common database errors
var (
	ErrNotFound          = internaldatabase.ErrNotFound
	ErrAlreadyExists     = internaldatabase.ErrAlreadyExists
	ErrInvalidInput      = internaldatabase.ErrInvalidInput
	ErrDatabase          = internaldatabase.ErrDatabase
	ErrInvalidVersion    = internaldatabase.ErrInvalidVersion
	ErrMaxServersReached = internaldatabase.ErrMaxServersReached
)

// InTransactionT is a generic helper that wraps InTransaction for functions returning a value.
func InTransactionT[T any](ctx context.Context, db Database, fn func(ctx context.Context, tx pgx.Tx) (T, error)) (T, error) {
	return internaldatabase.InTransactionT(ctx, db, fn)
}

// Package database provides the Database interface for registry operations.
// This package re-exports the internal database interface to allow enterprise
// implementations to wrap and extend the database layer.
//
// Example usage:
//
//	type MyDB struct {
//	    base database.Database
//	}
//
//	func (db *MyDB) ListServers(ctx context.Context, tx pgx.Tx, filter *database.ServerFilter, ...) ([]*apiv0.ServerResponse, string, error) {
//	    // Extract filter data from context (set by PrepareListContext)
//	    filterCtx := GetFilterContext(ctx)
//	    if filterCtx != nil {
//	        // Modify query to include filters
//	    }
//	    return db.base.ListServers(ctx, tx, filter, ...)
//	}
package database

import (
	"context"

	internaldatabase "github.com/agentregistry-dev/agentregistry/internal/registry/database"
	"github.com/jackc/pgx/v5"
)

// Database is the interface for registry database operations.
// Enterprise implementations can wrap this to add RBAC filtering.
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

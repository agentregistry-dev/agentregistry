package txutil

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

var ErrServiceDatabaseNotConfigured = errors.New("service database is not configured")

func Run(ctx context.Context, storeDB database.ServiceDatabase, fn func(context.Context, database.Store) error) error {
	if storeDB == nil {
		return ErrServiceDatabaseNotConfigured
	}

	return storeDB.InTransaction(ctx, fn)
}

func RunT[T any](ctx context.Context, storeDB database.ServiceDatabase, fn func(context.Context, database.Store) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := Run(ctx, storeDB, func(txCtx context.Context, store database.Store) error {
		result, fnErr = fn(txCtx, store)
		return fnErr
	})
	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}

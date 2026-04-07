package txutil

import (
	"context"
	"errors"

	"github.com/agentregistry-dev/agentregistry/pkg/registry/database"
)

var ErrStoreNotConfigured = errors.New("store is not configured")

func Run(ctx context.Context, tx database.Transactor, fn func(context.Context, database.Scope) error) error {
	if tx == nil {
		return ErrStoreNotConfigured
	}

	return tx.InTransaction(ctx, fn)
}

func RunT[T any](ctx context.Context, tx database.Transactor, fn func(context.Context, database.Scope) (T, error)) (T, error) {
	var result T
	var fnErr error

	err := Run(ctx, tx, func(txCtx context.Context, scope database.Scope) error {
		result, fnErr = fn(txCtx, scope)
		return fnErr
	})
	if err != nil {
		var zero T
		return zero, err
	}

	return result, nil
}

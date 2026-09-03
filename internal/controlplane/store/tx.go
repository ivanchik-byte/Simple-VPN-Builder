package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Transactor defines the interface for running atomic database transactions.
type Transactor interface {
	WithTx(ctx context.Context, fn func(q *Queries) error) error
}

// TxManager executes operations within a pgx transaction.
type TxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager creates a new transaction manager.
func NewTxManager(pool *pgxpool.Pool) *TxManager {
	return &TxManager{pool: pool}
}

// WithTx runs fn within a database transaction, rolling back on error and committing on success.
func (tm *TxManager) WithTx(ctx context.Context, fn func(q *Queries) error) (err error) {
	tx, err := tm.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	q := New(tx)
	if err = fn(q); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

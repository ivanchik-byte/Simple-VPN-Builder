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
	// WithAdvisoryLock runs fn inside a transaction with a pg_advisory_xact_lock on key.
	WithAdvisoryLock(ctx context.Context, key string, fn func(q *Queries) error) error
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
			// Use context.Background() so Rollback is never blocked by a cancelled ctx.
			_ = tx.Rollback(context.Background())
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(context.Background())
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

// WithAdvisoryLock runs fn in a transaction holding an advisory lock on key.
func (tm *TxManager) WithAdvisoryLock(ctx context.Context, key string, fn func(q *Queries) error) (err error) {
	tx, err := tm.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			// Use context.Background() so Rollback is never blocked by a cancelled ctx.
			_ = tx.Rollback(context.Background())
			panic(p)
		} else if err != nil {
			_ = tx.Rollback(context.Background())
		}
	}()

	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, key); err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}

	q := New(tx)
	if err = fn(q); err != nil {
		return err
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}

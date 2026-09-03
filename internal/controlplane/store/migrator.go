package store

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/ivanchik-byte/Simple-VPN-Builder/migrations"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// RunMigrations applies all pending database migrations.
func RunMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("create migration db driver: %w", err)
	}

	srcDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", srcDriver, "pgx", driver)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}

	return nil
}

// RollbackMigration rolls back the last migration step.
func RollbackMigration(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return fmt.Errorf("create migration db driver: %w", err)
	}

	srcDriver, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("create migration source driver: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", srcDriver, "pgx", driver)
	if err != nil {
		return fmt.Errorf("init migrator: %w", err)
	}

	if err := m.Steps(-1); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("rollback migration: %w", err)
	}

	return nil
}

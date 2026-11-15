package postgres

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
)

// Migrate applies every pending migration from migrations, an FS with the
// migration files at its root, and logs the version before and after.
//
// It runs on pool, so there is no second connection string. "Nothing to apply"
// is success. A dirty version, left by a failed migration, is an error:
// recovery is a manual `migrate force`. golang-migrate takes a
// Postgres advisory lock, so concurrent starts are safe.
func Migrate(ctx context.Context, pool *pgxpool.Pool, migrations fs.FS, log *slog.Logger) (err error) {
	m, closeAll, err := newMigrator(pool, migrations)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, closeAll()) }()

	before, err := cleanVersion(m)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("apply migrations from version %d: %w", before, err)
	}
	after, err := cleanVersion(m)
	if err != nil {
		return err
	}

	log.InfoContext(ctx, "migrations applied", "from", before, "to", after)
	return nil
}

// cleanVersion returns the applied version, 0 for an empty database, and an
// error if the version is dirty.
func cleanVersion(m *migrate.Migrate) (uint, error) {
	version, dirty, err := m.Version()
	switch {
	case errors.Is(err, migrate.ErrNilVersion):
		return 0, nil
	case err != nil:
		return 0, fmt.Errorf("read the migration version: %w", err)
	case dirty:
		return 0, fmt.Errorf("migration version %d is dirty: a migration failed; "+
			"fix it, then run `migrate force` by hand", version)
	}
	return version, nil
}

// newMigrator returns a migrator over pool and a function that releases it.
//
// The migration driver checks out one dedicated pool connection for as long
// as the migrator lives, so the release function must run on every path, or
// pool.Close() blocks forever (verified). It closes the migrator, which closes
// that connection and the *sql.DB, and then the *sql.DB again in case building
// the migrator failed half way; closing it twice is safe, and closing it does
// not close the pool.
func newMigrator(pool *pgxpool.Pool, migrations fs.FS) (*migrate.Migrate, func() error, error) {
	source, err := iofs.New(migrations, ".")
	if err != nil {
		return nil, nil, fmt.Errorf("read migrations: %w", err)
	}

	db := stdlib.OpenDBFromPool(pool)
	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("prepare the migration driver: %w", err), source.Close(), db.Close())
	}
	m, err := migrate.NewWithInstance("iofs", source, "pgx5", driver)
	if err != nil {
		return nil, nil, errors.Join(fmt.Errorf("prepare migrations: %w", err),
			source.Close(), driver.Close(), db.Close())
	}

	closeAll := func() error {
		sourceErr, dbErr := m.Close()
		return errors.Join(sourceErr, dbErr, db.Close())
	}
	return m, closeAll, nil
}

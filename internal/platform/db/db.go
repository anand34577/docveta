// Package db owns the PostgreSQL connection pool, migrations and transaction helpers.
package db

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Querier is satisfied by *pgxpool.Pool, *pgxpool.Conn and pgx.Tx so repositories
// can run inside or outside a transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns < 10 {
		cfg.MaxConns = 10
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connect to database: %w", err)
	}
	return pool, nil
}

// migrationLock is an arbitrary constant used as a Postgres advisory lock key so that
// only one instance runs migrations at a time.
const migrationLock int64 = 0x646f6376657461 // "docveta"

// Migrate applies all pending migrations under an advisory lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLock); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer conn.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", migrationLock) //nolint:errcheck

	sqldb := stdlib.OpenDBFromPool(pool)
	defer sqldb.Close()
	return runGoose(ctx, sqldb, log)
}

func runGoose(ctx context.Context, sqldb *sql.DB, log *slog.Logger) error {
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, migrationsFS())
	if err != nil {
		return err
	}
	results, err := provider.Up(ctx)
	for _, r := range results {
		log.Info("migration applied", "version", r.Source.Version, "file", r.Source.Path, "duration", r.Duration)
	}
	return err
}

// InTx runs fn inside a transaction, committing on success and rolling back on error or panic.
func InTx(ctx context.Context, pool *pgxpool.Pool, fn func(tx pgx.Tx) error) (err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()
	if err = fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// IsUniqueViolation reports whether err is a unique constraint violation.
func IsUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// IsForeignKeyViolation reports whether err is a foreign key violation.
func IsForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// IsNoRows reports whether err means "not found".
func IsNoRows(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

// MigrationVersions returns the applied and the latest available schema version.
func MigrationVersions(ctx context.Context, pool *pgxpool.Pool) (current, target int64, err error) {
	sqldb := stdlib.OpenDBFromPool(pool)
	defer sqldb.Close()
	provider, err := goose.NewProvider(goose.DialectPostgres, sqldb, migrationsFS())
	if err != nil {
		return 0, 0, err
	}
	return provider.GetVersions(ctx)
}

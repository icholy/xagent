package store

import (
	"context"
	"database/sql"
	"embed"

	"github.com/icholy/xagent/internal/store/sqlc"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
)

//go:embed sql/migrations/*.sql
var migrations embed.FS

func init() {
	goose.SetBaseFS(migrations)
	if err := goose.SetDialect("postgres"); err != nil {
		panic(err)
	}
	goose.SetLogger(goose.NopLogger())
}

// Store provides access to all database operations.
type Store struct {
	db *sql.DB
}

// New creates a new Store with the given database connection.
func New(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) q(tx *sql.Tx) *sqlc.Queries {
	if tx != nil {
		return sqlc.New(tx)
	}
	return sqlc.New(s.db)
}

// WithTx runs f within a transaction.
func (s *Store) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	if tx != nil {
		return f(tx)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return f(tx)
}

func Open(dsn string, migrate bool) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if migrate {
		if err := goose.Up(db, "sql/migrations"); err != nil {
			db.Close()
			return nil, err
		}
	}
	return db, nil
}

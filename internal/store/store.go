package store

import (
	"context"
	"database/sql"
	_ "embed"

	"github.com/icholy/xagent/internal/store/sqlc"
	_ "github.com/jackc/pgx/v5/stdlib"
)

//go:embed sql/schema.sql
var schema string

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

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

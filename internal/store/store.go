package store

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// DB is an interface that both *sql.DB and *sql.Tx implement
type DB interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// TxDB wraps *sql.DB with a reference to an optional transaction
type TxDB struct {
	db *sql.DB
	tx *sql.Tx
}

func NewTxDB(db *sql.DB, tx *sql.Tx) *TxDB {
	return &TxDB{db: db, tx: tx}
}

func (t *TxDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	if t.tx != nil {
		return t.tx.ExecContext(ctx, query, args...)
	}
	return t.db.ExecContext(ctx, query, args...)
}

func (t *TxDB) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	if t.tx != nil {
		return t.tx.QueryContext(ctx, query, args...)
	}
	return t.db.QueryContext(ctx, query, args...)
}

func (t *TxDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if t.tx != nil {
		return t.tx.QueryRowContext(ctx, query, args...)
	}
	return t.db.QueryRowContext(ctx, query, args...)
}

// Begin starts a new transaction (only works when not already in a transaction)
func (t *TxDB) Begin(ctx context.Context) (*sql.Tx, error) {
	return t.db.BeginTx(ctx, nil)
}

// WithTx executes the function f within a transaction
func WithTx(ctx context.Context, db *sql.DB, f func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := f(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// WithTxResult executes the function f within a transaction and returns a result
func WithTxResult[T any](ctx context.Context, db *sql.DB, f func(*sql.Tx) (T, error)) (T, error) {
	var zero T
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()

	result, err := f(tx)
	if err != nil {
		return zero, err
	}
	if err := tx.Commit(); err != nil {
		return zero, err
	}
	return result, nil
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path+"?mode=rwc&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	if err := migrate(db); err != nil {
		db.Close()
		return nil, err
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS tasks (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			name          TEXT NOT NULL DEFAULT '',
			parent        INTEGER NOT NULL DEFAULT 0,
			workspace     TEXT NOT NULL,
			prompts       TEXT NOT NULL,
			status        TEXT NOT NULL,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS logs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id    INTEGER NOT NULL,
			type       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);
		CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);

		CREATE TABLE IF NOT EXISTS task_links (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id    INTEGER NOT NULL,
			relevance  TEXT NOT NULL,
			url        TEXT NOT NULL,
			title      TEXT,
			notify     BOOLEAN DEFAULT FALSE,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);
		CREATE INDEX IF NOT EXISTS idx_task_links_task_id ON task_links(task_id);

		CREATE TABLE IF NOT EXISTS events (
			id          INTEGER PRIMARY KEY AUTOINCREMENT,
			description TEXT NOT NULL,
			data        TEXT NOT NULL,
			url         TEXT,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_events_url ON events(url);

		CREATE TABLE IF NOT EXISTS event_tasks (
			event_id INTEGER NOT NULL,
			task_id  INTEGER NOT NULL,
			PRIMARY KEY (event_id, task_id),
			FOREIGN KEY (event_id) REFERENCES events(id),
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);
		CREATE INDEX IF NOT EXISTS idx_event_tasks_task_id ON event_tasks(task_id);
	`)
	return err
}

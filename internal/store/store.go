package store

import (
	"context"
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

// Executor is an interface that both *sql.DB and *sql.Tx implement.
// It allows repository methods to work with either.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// WithTx runs f within a transaction. If tx is non-nil, it uses that transaction.
// If tx is nil, it creates a new transaction. The callback is responsible for
// committing the transaction. If an error is returned, the transaction is rolled back.
func WithTx(ctx context.Context, db *sql.DB, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	if tx != nil {
		return f(tx)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	return f(tx)
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
			command       TEXT NOT NULL DEFAULT '',
			version       INTEGER NOT NULL DEFAULT 0,
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

		CREATE TABLE IF NOT EXISTS workspaces (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			runner_id  TEXT NOT NULL,
			name       TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE INDEX IF NOT EXISTS idx_workspaces_runner_id ON workspaces(runner_id);
	`)
	return err
}

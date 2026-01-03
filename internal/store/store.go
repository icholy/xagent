package store

import (
	"database/sql"

	_ "github.com/mattn/go-sqlite3"
)

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
			id            TEXT PRIMARY KEY,
			name          TEXT NOT NULL DEFAULT '',
			parent        TEXT NOT NULL DEFAULT '',
			workspace     TEXT NOT NULL,
			prompts       TEXT NOT NULL,
			status        TEXT NOT NULL,
			created_at    DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at    DATETIME DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE IF NOT EXISTS logs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id    TEXT NOT NULL,
			type       TEXT NOT NULL,
			content    TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);
		CREATE INDEX IF NOT EXISTS idx_logs_task_id ON logs(task_id);

		CREATE TABLE IF NOT EXISTS task_links (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			task_id    TEXT NOT NULL,
			relevance  TEXT NOT NULL,
			url        TEXT NOT NULL,
			title      TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created    BOOLEAN DEFAULT FALSE,
			FOREIGN KEY (task_id) REFERENCES tasks(id)
		);
		CREATE INDEX IF NOT EXISTS idx_task_links_task_id ON task_links(task_id);
	`)
	return err
}

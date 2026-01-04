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
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			created    BOOLEAN DEFAULT FALSE,
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

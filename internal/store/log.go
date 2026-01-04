package store

import (
	"database/sql"
	"time"
)

type Log struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	Type      string    `json:"type"`
	Content   string    `json:"content"`
	CreatedAt time.Time `json:"created_at"`
}

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) Create(log *Log) error {
	result, err := r.db.Exec(`
		INSERT INTO logs (task_id, type, content, created_at)
		VALUES (?, ?, ?, ?)
	`, log.TaskID, log.Type, log.Content, time.Now())
	if err != nil {
		return err
	}
	log.ID, _ = result.LastInsertId()
	return nil
}

func (r *LogRepository) CreateBatch(logs []*Log) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT INTO logs (task_id, type, content, created_at)
		VALUES (?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	now := time.Now()
	for _, log := range logs {
		result, err := stmt.Exec(log.TaskID, log.Type, log.Content, now)
		if err != nil {
			return err
		}
		log.ID, _ = result.LastInsertId()
	}

	return tx.Commit()
}

func (r *LogRepository) ListByTask(taskID int64) ([]*Log, error) {
	rows, err := r.db.Query(`
		SELECT id, task_id, type, content, created_at
		FROM logs WHERE task_id = ? ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*Log
	for rows.Next() {
		var log Log
		if err := rows.Scan(&log.ID, &log.TaskID, &log.Type, &log.Content, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, rows.Err()
}

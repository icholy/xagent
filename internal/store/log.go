package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
)

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) Create(ctx context.Context, log *model.Log) error {
	result, err := r.db.ExecContext(ctx, `
		INSERT INTO logs (task_id, type, content, created_at)
		VALUES (?, ?, ?, ?)
	`, log.TaskID, log.Type, log.Content, time.Now())
	if err != nil {
		return err
	}
	log.ID, _ = result.LastInsertId()
	return nil
}

func (r *LogRepository) ListByTask(ctx context.Context, taskID int64) ([]*model.Log, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, task_id, type, content, created_at
		FROM logs WHERE task_id = ? ORDER BY created_at ASC
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*model.Log
	for rows.Next() {
		var log model.Log
		if err := rows.Scan(&log.ID, &log.TaskID, &log.Type, &log.Content, &log.CreatedAt); err != nil {
			return nil, err
		}
		logs = append(logs, &log)
	}
	return logs, rows.Err()
}

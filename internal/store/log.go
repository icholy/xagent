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

func (r *LogRepository) txdb(tx *sql.Tx) *TxDB {
	return NewTxDB(r.db, tx)
}

func (r *LogRepository) Create(tx *sql.Tx, log *model.Log) error {
	result, err := r.txdb(tx).ExecContext(context.Background(), `
		INSERT INTO logs (task_id, type, content, created_at)
		VALUES (?, ?, ?, ?)
	`, log.TaskID, log.Type, log.Content, time.Now())
	if err != nil {
		return err
	}
	log.ID, _ = result.LastInsertId()
	return nil
}

func (r *LogRepository) CreateBatch(tx *sql.Tx, logs []*model.Log) error {
	// If no tx was provided, create one
	if tx == nil {
		return WithTx(context.Background(), r.db, func(innerTx *sql.Tx) error {
			return r.CreateBatch(innerTx, logs)
		})
	}

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

	return nil
}

func (r *LogRepository) ListByTask(tx *sql.Tx, taskID int64) ([]*model.Log, error) {
	rows, err := r.txdb(tx).QueryContext(context.Background(), `
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

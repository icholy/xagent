package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

type LogRepository struct {
	db *sql.DB
}

func NewLogRepository(db *sql.DB) *LogRepository {
	return &LogRepository{db: db}
}

func (r *LogRepository) queries(tx *sql.Tx) *sqlc.Queries {
	if tx != nil {
		return sqlc.New(tx)
	}
	return sqlc.New(r.db)
}

func (r *LogRepository) Create(ctx context.Context, tx *sql.Tx, log *model.Log) error {
	id, err := r.queries(tx).CreateLog(ctx, sqlc.CreateLogParams{
		TaskID:    log.TaskID,
		Type:      log.Type,
		Content:   log.Content,
		CreatedAt: sql.NullTime{Time: time.Now(), Valid: true},
	})
	if err != nil {
		return err
	}
	log.ID = id
	return nil
}

func (r *LogRepository) ListByTask(ctx context.Context, tx *sql.Tx, taskID int64, owner string) ([]*model.Log, error) {
	rows, err := r.queries(tx).ListLogsByTask(ctx, sqlc.ListLogsByTaskParams{
		TaskID: taskID,
		Owner:  owner,
	})
	if err != nil {
		return nil, err
	}
	logs := make([]*model.Log, len(rows))
	for i, row := range rows {
		logs[i] = &model.Log{
			ID:        row.ID,
			TaskID:    row.TaskID,
			Type:      row.Type,
			Content:   row.Content,
			CreatedAt: row.CreatedAt.Time,
		}
	}
	return logs, nil
}

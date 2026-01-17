package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
)

type SessionRepository struct {
	db *sql.DB
}

func NewSessionRepository(db *sql.DB) *SessionRepository {
	return &SessionRepository{db: db}
}

func (r *SessionRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *SessionRepository) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	return WithTx(ctx, r.db, tx, f)
}

func (r *SessionRepository) Create(ctx context.Context, tx *sql.Tx, session *model.Session) error {
	now := time.Now()
	_, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO sessions (id, user_id, expires_at, created_at)
		VALUES (?, ?, ?, ?)
	`, session.ID, session.UserID, session.ExpiresAt, now)
	if err != nil {
		return err
	}
	session.CreatedAt = now
	return nil
}

func (r *SessionRepository) Get(ctx context.Context, tx *sql.Tx, id string) (*model.Session, error) {
	row := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, user_id, expires_at, created_at
		FROM sessions WHERE id = ?
	`, id)
	return r.scanSession(row)
}

func (r *SessionRepository) Delete(ctx context.Context, tx *sql.Tx, id string) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM sessions WHERE id = ?`, id)
	return err
}

func (r *SessionRepository) DeleteByUser(ctx context.Context, tx *sql.Tx, userID int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (r *SessionRepository) DeleteExpired(ctx context.Context, tx *sql.Tx) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM sessions WHERE expires_at < ?`, time.Now())
	return err
}

func (r *SessionRepository) scanSession(row *sql.Row) (*model.Session, error) {
	var session model.Session
	err := row.Scan(
		&session.ID,
		&session.UserID,
		&session.ExpiresAt,
		&session.CreatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &session, nil
}

package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
)

type UserRepository struct {
	db *sql.DB
}

func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

func (r *UserRepository) exec(tx *sql.Tx) Executor {
	if tx != nil {
		return tx
	}
	return r.db
}

func (r *UserRepository) WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error {
	return WithTx(ctx, r.db, tx, f)
}

func (r *UserRepository) Create(ctx context.Context, tx *sql.Tx, user *model.User) error {
	now := time.Now()
	result, err := r.exec(tx).ExecContext(ctx, `
		INSERT INTO users (google_id, email, name, picture, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
	`, user.GoogleID, user.Email, user.Name, user.Picture, now, now)
	if err != nil {
		return err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return err
	}
	user.ID = id
	user.CreatedAt = now
	user.UpdatedAt = now
	return nil
}

func (r *UserRepository) Get(ctx context.Context, tx *sql.Tx, id int64) (*model.User, error) {
	row := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, google_id, email, name, picture, created_at, updated_at
		FROM users WHERE id = ?
	`, id)
	return r.scanUser(row)
}

func (r *UserRepository) GetByGoogleID(ctx context.Context, tx *sql.Tx, googleID string) (*model.User, error) {
	row := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, google_id, email, name, picture, created_at, updated_at
		FROM users WHERE google_id = ?
	`, googleID)
	return r.scanUser(row)
}

func (r *UserRepository) GetByEmail(ctx context.Context, tx *sql.Tx, email string) (*model.User, error) {
	row := r.exec(tx).QueryRowContext(ctx, `
		SELECT id, google_id, email, name, picture, created_at, updated_at
		FROM users WHERE email = ?
	`, email)
	return r.scanUser(row)
}

func (r *UserRepository) Put(ctx context.Context, tx *sql.Tx, user *model.User) error {
	user.UpdatedAt = time.Now()
	_, err := r.exec(tx).ExecContext(ctx, `
		UPDATE users SET google_id = ?, email = ?, name = ?, picture = ?, updated_at = ?
		WHERE id = ?
	`, user.GoogleID, user.Email, user.Name, user.Picture, user.UpdatedAt, user.ID)
	return err
}

func (r *UserRepository) Delete(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := r.exec(tx).ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (r *UserRepository) scanUser(row *sql.Row) (*model.User, error) {
	var user model.User
	err := row.Scan(
		&user.ID,
		&user.GoogleID,
		&user.Email,
		&user.Name,
		&user.Picture,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

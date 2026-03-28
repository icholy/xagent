package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) UpsertUser(ctx context.Context, tx *sql.Tx, user *model.User) error {
	row, err := s.q(tx).UpsertUser(ctx, sqlc.UpsertUserParams{
		ID:    user.ID,
		Email: user.Email,
		Name:  user.Name,
	})
	if err != nil {
		return err
	}
	user.CreatedAt = row.CreatedAt
	user.UpdatedAt = row.UpdatedAt
	return nil
}

func (s *Store) GetUser(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
	row, err := s.q(tx).GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return toModelUser(row), nil
}

func (s *Store) GetUserByEmail(ctx context.Context, tx *sql.Tx, email string) (*model.User, error) {
	row, err := s.q(tx).GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return toModelUser(row), nil
}

func toModelUser(row sqlc.User) *model.User {
	return &model.User{
		ID:        row.ID,
		Email:     row.Email,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
}

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

func (s *Store) GetUserByGitHubUserID(ctx context.Context, tx *sql.Tx, githubUserID int64) (*model.User, error) {
	row, err := s.q(tx).GetUserByGitHubUserID(ctx, sql.NullInt64{Int64: githubUserID, Valid: true})
	if err != nil {
		return nil, err
	}
	return toModelUser(row), nil
}

func (s *Store) LinkGitHubAccount(ctx context.Context, tx *sql.Tx, userID string, githubUserID int64, githubUsername string) error {
	return s.q(tx).LinkGitHubAccount(ctx, sqlc.LinkGitHubAccountParams{
		ID:             userID,
		GithubUserID:   sql.NullInt64{Int64: githubUserID, Valid: true},
		GithubUsername: githubUsername,
	})
}

func (s *Store) UnlinkGitHubAccount(ctx context.Context, tx *sql.Tx, userID string) error {
	return s.q(tx).UnlinkGitHubAccount(ctx, userID)
}

func (s *Store) UpdateGitHubUsername(ctx context.Context, tx *sql.Tx, githubUserID int64, username string) error {
	return s.q(tx).UpdateGitHubUsername(ctx, sqlc.UpdateGitHubUsernameParams{
		GithubUsername: username,
		GithubUserID:   sql.NullInt64{Int64: githubUserID, Valid: true},
	})
}

func toModelUser(row sqlc.User) *model.User {
	u := &model.User{
		ID:             row.ID,
		Email:          row.Email,
		Name:           row.Name,
		GitHubUsername: row.GithubUsername,
		CreatedAt:      row.CreatedAt,
		UpdatedAt:      row.UpdatedAt,
	}
	if row.GithubUserID.Valid {
		u.GitHubUserID = row.GithubUserID.Int64
	}
	return u
}

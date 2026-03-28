package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) UpsertUser(ctx context.Context, tx *sql.Tx, user *model.User) error {
	row, err := s.q(tx).UpsertUser(ctx, sqlc.UpsertUserParams{
		ID:             user.ID,
		Email:          user.Email,
		Name:           user.Name,
		GithubUserID:   sql.NullInt64{Int64: user.GitHubUserID, Valid: user.GitHubUserID != 0},
		GithubUsername: sql.NullString{String: user.GitHubUsername, Valid: user.GitHubUsername != ""},
		DefaultOrgID:   sql.NullInt64{Int64: user.DefaultOrgID, Valid: user.DefaultOrgID != 0},
	})
	if err != nil {
		return err
	}
	user.CreatedAt = row.CreatedAt
	user.UpdatedAt = row.UpdatedAt
	if row.DefaultOrgID.Valid {
		user.DefaultOrgID = row.DefaultOrgID.Int64
	}
	return nil
}

func (s *Store) GetUser(ctx context.Context, tx *sql.Tx, id string) (*model.User, error) {
	row, err := s.q(tx).GetUser(ctx, id)
	if err != nil {
		return nil, err
	}
	return toModelUserRow(row.ID, row.Email, row.Name, row.GithubUserID, row.GithubUsername, row.DefaultOrgID, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) GetUserByEmail(ctx context.Context, tx *sql.Tx, email string) (*model.User, error) {
	row, err := s.q(tx).GetUserByEmail(ctx, email)
	if err != nil {
		return nil, err
	}
	return toModelUserRow(row.ID, row.Email, row.Name, row.GithubUserID, row.GithubUsername, row.DefaultOrgID, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) GetUserByGitHubUserID(ctx context.Context, tx *sql.Tx, githubUserID int64) (*model.User, error) {
	row, err := s.q(tx).GetUserByGitHubUserID(ctx, sql.NullInt64{Int64: githubUserID, Valid: true})
	if err != nil {
		return nil, err
	}
	return toModelUserRow(row.ID, row.Email, row.Name, row.GithubUserID, row.GithubUsername, row.DefaultOrgID, row.CreatedAt, row.UpdatedAt), nil
}

func (s *Store) LinkGitHubAccount(ctx context.Context, tx *sql.Tx, userID string, githubUserID int64, githubUsername string) error {
	return s.q(tx).LinkGitHubAccount(ctx, sqlc.LinkGitHubAccountParams{
		ID:             userID,
		GithubUserID:   sql.NullInt64{Int64: githubUserID, Valid: true},
		GithubUsername: sql.NullString{String: githubUsername, Valid: true},
	})
}

func (s *Store) UnlinkGitHubAccount(ctx context.Context, tx *sql.Tx, userID string) error {
	return s.q(tx).UnlinkGitHubAccount(ctx, userID)
}

func (s *Store) UpdateGitHubUsername(ctx context.Context, tx *sql.Tx, githubUserID int64, username string) error {
	return s.q(tx).UpdateGitHubUsername(ctx, sqlc.UpdateGitHubUsernameParams{
		GithubUsername: sql.NullString{String: username, Valid: true},
		GithubUserID:   sql.NullInt64{Int64: githubUserID, Valid: true},
	})
}

func (s *Store) UpdateDefaultOrgID(ctx context.Context, tx *sql.Tx, userID string, orgID int64) error {
	return s.q(tx).UpdateDefaultOrgID(ctx, sqlc.UpdateDefaultOrgIDParams{
		ID:           userID,
		DefaultOrgID: sql.NullInt64{Int64: orgID, Valid: orgID != 0},
	})
}

func toModelUserRow(id, email, name string, githubUserID sql.NullInt64, githubUsername sql.NullString, defaultOrgID sql.NullInt64, createdAt, updatedAt time.Time) *model.User {
	u := &model.User{
		ID:             id,
		Email:          email,
		Name:           name,
		GitHubUsername: githubUsername.String,
		CreatedAt:      createdAt,
		UpdatedAt:      updatedAt,
	}
	if githubUserID.Valid {
		u.GitHubUserID = githubUserID.Int64
	}
	if defaultOrgID.Valid {
		u.DefaultOrgID = defaultOrgID.Int64
	}
	return u
}

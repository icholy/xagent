package store

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateGitHubAccount(ctx context.Context, tx *sql.Tx, account *model.GitHubAccount) error {
	row, err := s.q(tx).CreateGitHubAccount(ctx, sqlc.CreateGitHubAccountParams{
		Owner:          account.Owner,
		GithubUserID:   account.GitHubUserID,
		GithubUsername: account.GitHubUsername,
	})
	if err != nil {
		return err
	}
	account.ID = row.ID
	account.CreatedAt = row.CreatedAt
	return nil
}

func (s *Store) GetGitHubAccountByOwner(ctx context.Context, tx *sql.Tx, owner string) (*model.GitHubAccount, error) {
	row, err := s.q(tx).GetGitHubAccountByOwner(ctx, owner)
	if err != nil {
		return nil, err
	}
	return toModelGitHubAccount(row), nil
}

func (s *Store) GetGitHubAccountByGitHubUserID(ctx context.Context, tx *sql.Tx, githubUserID int64) (*model.GitHubAccount, error) {
	row, err := s.q(tx).GetGitHubAccountByGitHubUserID(ctx, githubUserID)
	if err != nil {
		return nil, err
	}
	return toModelGitHubAccount(row), nil
}

func (s *Store) DeleteGitHubAccount(ctx context.Context, tx *sql.Tx, owner string) error {
	return s.q(tx).DeleteGitHubAccount(ctx, owner)
}

func (s *Store) UpdateGitHubUsername(ctx context.Context, tx *sql.Tx, githubUserID int64, username string) error {
	return s.q(tx).UpdateGitHubUsername(ctx, sqlc.UpdateGitHubUsernameParams{
		GithubUsername: username,
		GithubUserID:   githubUserID,
	})
}

func toModelGitHubAccount(row sqlc.GithubAccount) *model.GitHubAccount {
	return &model.GitHubAccount{
		ID:             row.ID,
		Owner:          row.Owner,
		GitHubUserID:   row.GithubUserID,
		GitHubUsername: row.GithubUsername,
		CreatedAt:      row.CreatedAt,
	}
}

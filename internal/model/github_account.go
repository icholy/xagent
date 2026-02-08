package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// GitHubAccount represents a linked GitHub account.
type GitHubAccount struct {
	ID             int64     `json:"id"`
	Owner          string    `json:"owner"`
	GitHubUserID   int64     `json:"github_user_id"`
	GitHubUsername string    `json:"github_username"`
	CreatedAt      time.Time `json:"created_at"`
}

// Proto converts a GitHubAccount to its protobuf representation.
func (a *GitHubAccount) Proto() *xagentv1.GitHubAccount {
	return &xagentv1.GitHubAccount{
		Id:             a.ID,
		GithubUserId:   a.GitHubUserID,
		GithubUsername: a.GitHubUsername,
		CreatedAt:      timestamppb.New(a.CreatedAt),
	}
}

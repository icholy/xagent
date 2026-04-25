//go:generate go tool moq -out store_moq_test.go . Store

package webhookserver

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
)

// Store is the subset of store.Store used by webhook handlers.
type Store interface {
	GetUserByGitHubUserID(ctx context.Context, tx *sql.Tx, githubUserID int64) (*model.User, error)
	UpdateGitHubUsername(ctx context.Context, tx *sql.Tx, githubUserID int64, username string) error
	GetOrgAtlassianWebhookSecret(ctx context.Context, tx *sql.Tx, orgID int64) (string, error)
	GetUserByAtlassianAccountID(ctx context.Context, tx *sql.Tx, atlassianAccountID string) (*model.User, error)
}

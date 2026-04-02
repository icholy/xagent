//go:generate go tool moq -out store_moq_test.go . Store

package eventrouter

import (
	"context"
	"database/sql"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store"
)

// Store is the subset of store.Store used by the event router.
type Store interface {
	FindSubscribedLinksByURLForUser(ctx context.Context, tx *sql.Tx, url string, userID string) ([]store.LinkWithOrg, error)
	CreateEvent(ctx context.Context, tx *sql.Tx, event *model.Event) error
	WithTx(ctx context.Context, tx *sql.Tx, f func(tx *sql.Tx) error) error
	AddEventTask(ctx context.Context, tx *sql.Tx, eventID int64, taskID int64) error
	GetTaskForUpdate(ctx context.Context, tx *sql.Tx, id int64, orgID int64) (*model.Task, error)
	UpdateTask(ctx context.Context, tx *sql.Tx, task *model.Task) error
	CreateLog(ctx context.Context, tx *sql.Tx, log *model.Log) error
}

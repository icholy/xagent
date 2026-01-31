package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateWebhook(ctx context.Context, tx *sql.Tx, webhook *model.Webhook) error {
	now := time.Now()
	err := s.q(tx).CreateWebhook(ctx, sqlc.CreateWebhookParams{
		Uuid:      webhook.UUID,
		Secret:    webhook.Secret,
		Owner:     webhook.Owner,
		CreatedAt: now,
	})
	if err != nil {
		return err
	}
	webhook.CreatedAt = now
	return nil
}

func (s *Store) GetWebhook(ctx context.Context, tx *sql.Tx, uuid string, owner string) (*model.Webhook, error) {
	row, err := s.q(tx).GetWebhook(ctx, sqlc.GetWebhookParams{
		Uuid:  uuid,
		Owner: owner,
	})
	if err != nil {
		return nil, err
	}
	return toModelWebhook(row), nil
}

func (s *Store) ListWebhooks(ctx context.Context, tx *sql.Tx, owner string) ([]*model.Webhook, error) {
	rows, err := s.q(tx).ListWebhooks(ctx, owner)
	if err != nil {
		return nil, err
	}
	return toModelWebhooks(rows), nil
}

func (s *Store) DeleteWebhook(ctx context.Context, tx *sql.Tx, uuid string, owner string) error {
	return s.q(tx).DeleteWebhook(ctx, sqlc.DeleteWebhookParams{
		Uuid:  uuid,
		Owner: owner,
	})
}

func toModelWebhook(row sqlc.Webhook) *model.Webhook {
	return &model.Webhook{
		UUID:      row.Uuid,
		Secret:    row.Secret,
		Owner:     row.Owner,
		CreatedAt: row.CreatedAt,
	}
}

func toModelWebhooks(rows []sqlc.Webhook) []*model.Webhook {
	webhooks := make([]*model.Webhook, len(rows))
	for i, row := range rows {
		webhooks[i] = toModelWebhook(row)
	}
	return webhooks
}

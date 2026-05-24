package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) UpsertPendingIntegration(ctx context.Context, tx *sql.Tx, p *model.PendingIntegration) error {
	options, err := json.Marshal(p.Options)
	if err != nil {
		return fmt.Errorf("marshal options: %w", err)
	}
	now := time.Now().UTC()
	if err := s.q(tx).UpsertPendingIntegration(ctx, sqlc.UpsertPendingIntegrationParams{
		Type:       string(p.Type),
		ExternalID: p.ExternalID,
		Options:    options,
		CreatedAt:  now,
	}); err != nil {
		return err
	}
	p.CreatedAt = now
	return nil
}

func (s *Store) GetPendingIntegration(ctx context.Context, tx *sql.Tx, typ model.PendingIntegrationType, externalID string) (*model.PendingIntegration, error) {
	row, err := s.q(tx).GetPendingIntegration(ctx, sqlc.GetPendingIntegrationParams{
		Type:       string(typ),
		ExternalID: externalID,
	})
	if err != nil {
		return nil, err
	}
	var options model.PendingIntegrationOptions
	if err := json.Unmarshal(row.Options, &options); err != nil {
		return nil, fmt.Errorf("unmarshal options: %w", err)
	}
	return &model.PendingIntegration{
		Type:       model.PendingIntegrationType(row.Type),
		ExternalID: row.ExternalID,
		Options:    options,
		CreatedAt:  row.CreatedAt,
	}, nil
}

func (s *Store) DeletePendingIntegration(ctx context.Context, tx *sql.Tx, typ model.PendingIntegrationType, externalID string) error {
	return s.q(tx).DeletePendingIntegration(ctx, sqlc.DeletePendingIntegrationParams{
		Type:       string(typ),
		ExternalID: externalID,
	})
}

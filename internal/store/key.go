package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateKey(ctx context.Context, tx *sql.Tx, key *model.Key) error {
	now := time.Now()
	var expiresAt sql.NullTime
	if key.ExpiresAt != nil {
		expiresAt = sql.NullTime{Time: *key.ExpiresAt, Valid: true}
	}
	err := s.q(tx).CreateKey(ctx, sqlc.CreateKeyParams{
		ID:        key.ID,
		Name:      key.Name,
		TokenHash: key.TokenHash,
		OrgID:     key.OrgID,
		ExpiresAt: expiresAt,
		CreatedAt: now,
	})
	if err != nil {
		return err
	}
	key.CreatedAt = now
	return nil
}

func (s *Store) GetKey(ctx context.Context, tx *sql.Tx, id string, orgID int64) (*model.Key, error) {
	row, err := s.q(tx).GetKey(ctx, sqlc.GetKeyParams{
		ID:    id,
		OrgID: orgID,
	})
	if err != nil {
		return nil, err
	}
	return toModelKey(row), nil
}

func (s *Store) GetKeyByHash(ctx context.Context, tx *sql.Tx, hash string) (*model.Key, error) {
	row, err := s.q(tx).GetKeyByHash(ctx, hash)
	if err != nil {
		return nil, err
	}
	return toModelKey(row), nil
}

func (s *Store) ListKeys(ctx context.Context, tx *sql.Tx, orgID int64) ([]*model.Key, error) {
	rows, err := s.q(tx).ListKeys(ctx, orgID)
	if err != nil {
		return nil, err
	}
	return toModelKeys(rows), nil
}

func (s *Store) DeleteKey(ctx context.Context, tx *sql.Tx, id string, orgID int64) error {
	return s.q(tx).DeleteKey(ctx, sqlc.DeleteKeyParams{
		ID:    id,
		OrgID: orgID,
	})
}

func toModelKey(row sqlc.Key) *model.Key {
	key := &model.Key{
		ID:        row.ID,
		Name:      row.Name,
		TokenHash: row.TokenHash,
		OrgID:     row.OrgID,
		CreatedAt: row.CreatedAt,
	}
	if row.ExpiresAt.Valid {
		key.ExpiresAt = &row.ExpiresAt.Time
	}
	return key
}

func toModelKeys(rows []sqlc.Key) []*model.Key {
	keys := make([]*model.Key, len(rows))
	for i, row := range rows {
		keys[i] = toModelKey(row)
	}
	return keys
}

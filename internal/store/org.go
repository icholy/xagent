package store

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateOrg(ctx context.Context, tx *sql.Tx, org *model.Org) error {
	now := time.Now()
	id, err := s.q(tx).CreateOrg(ctx, sqlc.CreateOrgParams{
		Name:      org.Name,
		OwnerID:   org.OwnerID,
		CreatedAt: now,
	})
	if err != nil {
		return err
	}
	org.ID = id
	org.CreatedAt = now
	return nil
}

func (s *Store) GetOrg(ctx context.Context, tx *sql.Tx, id int64) (*model.Org, error) {
	row, err := s.q(tx).GetOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	return &model.Org{
		ID:        row.ID,
		Name:      row.Name,
		OwnerID:   row.OwnerID,
		CreatedAt: row.CreatedAt,
	}, nil
}

func (s *Store) ListOrgsByUser(ctx context.Context, tx *sql.Tx, userID string) ([]*model.Org, error) {
	rows, err := s.q(tx).ListOrgsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	orgs := make([]*model.Org, len(rows))
	for i, row := range rows {
		orgs[i] = &model.Org{
			ID:        row.ID,
			Name:      row.Name,
			OwnerID:   row.OwnerID,
			CreatedAt: row.CreatedAt,
		}
	}
	return orgs, nil
}

func (s *Store) DeleteOrg(ctx context.Context, tx *sql.Tx, id int64, ownerID string) error {
	return s.q(tx).DeleteOrg(ctx, sqlc.DeleteOrgParams{
		ID:      id,
		OwnerID: ownerID,
	})
}

func (s *Store) CreateOrgMember(ctx context.Context, tx *sql.Tx, orgID int64, userID string) error {
	return s.q(tx).CreateOrgMember(ctx, sqlc.CreateOrgMemberParams{
		OrgID:  orgID,
		UserID: userID,
	})
}

func (s *Store) RemoveOrgMember(ctx context.Context, tx *sql.Tx, orgID int64, userID string) error {
	return s.q(tx).RemoveOrgMember(ctx, sqlc.RemoveOrgMemberParams{
		OrgID:  orgID,
		UserID: userID,
	})
}

func (s *Store) ListOrgMembers(ctx context.Context, tx *sql.Tx, orgID int64) ([]*model.OrgMember, error) {
	rows, err := s.q(tx).ListOrgMembers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members := make([]*model.OrgMember, len(rows))
	for i, row := range rows {
		members[i] = &model.OrgMember{
			ID:        row.ID,
			OrgID:     row.OrgID,
			UserID:    row.UserID,
			Email:     row.Email,
			Name:      row.Name,
			CreatedAt: row.CreatedAt,
		}
	}
	return members, nil
}

func (s *Store) IsOrgMember(ctx context.Context, tx *sql.Tx, orgID int64, userID string) (bool, error) {
	return s.q(tx).IsOrgMember(ctx, sqlc.IsOrgMemberParams{
		OrgID:  orgID,
		UserID: userID,
	})
}

func (s *Store) UpsertUser(ctx context.Context, tx *sql.Tx, user *model.User) error {
	now := time.Now()
	return s.q(tx).UpsertUser(ctx, sqlc.UpsertUserParams{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		CreatedAt: now,
		UpdatedAt: now,
	})
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

func (s *Store) UpdateUserDefaultOrg(ctx context.Context, tx *sql.Tx, userID string, orgID int64) error {
	return s.q(tx).UpdateUserDefaultOrg(ctx, sqlc.UpdateUserDefaultOrgParams{
		DefaultOrgID: sql.NullInt64{Int64: orgID, Valid: orgID != 0},
		UpdatedAt:    time.Now(),
		ID:           userID,
	})
}

// ResolveOrgOwner verifies that a user is a member of the given org and returns the
// org ID as a string suitable for use as the owner field in queries.
func (s *Store) ResolveOrgOwner(ctx context.Context, tx *sql.Tx, orgID int64, userID string) (string, error) {
	ok, err := s.IsOrgMember(ctx, tx, orgID, userID)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", fmt.Errorf("user %s is not a member of org %d", userID, orgID)
	}
	return strconv.FormatInt(orgID, 10), nil
}

func toModelUser(row sqlc.User) *model.User {
	u := &model.User{
		ID:        row.ID,
		Email:     row.Email,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
		UpdatedAt: row.UpdatedAt,
	}
	if row.DefaultOrgID.Valid {
		u.DefaultOrgID = row.DefaultOrgID.Int64
	}
	return u
}

package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/store/sqlc"
)

func (s *Store) CreateOrg(ctx context.Context, tx *sql.Tx, org *model.Org) error {
	now := time.Now()
	id, err := s.q(tx).CreateOrg(ctx, sqlc.CreateOrgParams{
		Name:      org.Name,
		Owner:     org.Owner,
		CreatedAt: now,
		UpdatedAt: now,
	})
	if err != nil {
		return err
	}
	org.ID = id
	org.CreatedAt = now
	org.UpdatedAt = now
	return nil
}

func (s *Store) GetOrg(ctx context.Context, tx *sql.Tx, id int64) (*model.Org, error) {
	row, err := s.q(tx).GetOrg(ctx, id)
	if err != nil {
		return nil, err
	}
	return toModelOrg(row), nil
}

func (s *Store) ListOrgsByMember(ctx context.Context, tx *sql.Tx, userID string) ([]*model.Org, error) {
	rows, err := s.q(tx).ListOrgsByMember(ctx, userID)
	if err != nil {
		return nil, err
	}
	orgs := make([]*model.Org, len(rows))
	for i, row := range rows {
		orgs[i] = toModelOrg(row)
	}
	return orgs, nil
}

func (s *Store) UpdateOrg(ctx context.Context, tx *sql.Tx, org *model.Org) error {
	org.UpdatedAt = time.Now()
	return s.q(tx).UpdateOrg(ctx, sqlc.UpdateOrgParams{
		ID:        org.ID,
		Name:      org.Name,
		UpdatedAt: org.UpdatedAt,
	})
}

func (s *Store) ArchiveOrg(ctx context.Context, tx *sql.Tx, id int64) error {
	return s.q(tx).ArchiveOrg(ctx, id)
}

func (s *Store) AddOrgMember(ctx context.Context, tx *sql.Tx, member *model.OrgMember) error {
	now := time.Now()
	err := s.q(tx).AddOrgMember(ctx, sqlc.AddOrgMemberParams{
		OrgID:     member.OrgID,
		UserID:    member.UserID,
		Role:      member.Role,
		CreatedAt: now,
	})
	if err != nil {
		return err
	}
	member.CreatedAt = now
	return nil
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
		members[i] = toModelOrgMember(row)
	}
	return members, nil
}

func (s *Store) ListOrgMembersWithUsers(ctx context.Context, tx *sql.Tx, orgID int64) ([]*model.OrgMemberWithUser, error) {
	rows, err := s.q(tx).ListOrgMembersWithUsers(ctx, orgID)
	if err != nil {
		return nil, err
	}
	members := make([]*model.OrgMemberWithUser, len(rows))
	for i, row := range rows {
		members[i] = &model.OrgMemberWithUser{
			OrgMember: model.OrgMember{
				OrgID:     row.OrgID,
				UserID:    row.UserID,
				Role:      row.Role,
				CreatedAt: row.CreatedAt,
			},
			Email: row.Email,
			Name:  row.Name,
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

// ResolveOrgOwner verifies that the user is a member of the given org and
// returns the org ID. Returns an error if the user is not a member.
func (s *Store) ResolveOrgOwner(ctx context.Context, tx *sql.Tx, orgID int64, userID string) (int64, error) {
	ok, err := s.IsOrgMember(ctx, tx, orgID, userID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, fmt.Errorf("user %s is not a member of org %d", userID, orgID)
	}
	return orgID, nil
}

func toModelOrg(row sqlc.Org) *model.Org {
	return &model.Org{
		ID:                row.ID,
		Name:              row.Name,
		Owner:             row.Owner,
		Archived:          row.Archived,
		JiraWebhookSecret: row.JiraWebhookSecret,
		CreatedAt:         row.CreatedAt,
		UpdatedAt:         row.UpdatedAt,
	}
}

func toModelOrgMember(row sqlc.OrgMember) *model.OrgMember {
	return &model.OrgMember{
		OrgID:     row.OrgID,
		UserID:    row.UserID,
		Role:      row.Role,
		CreatedAt: row.CreatedAt,
	}
}

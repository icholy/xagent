package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Org represents an organisation that owns resources.
type Org struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Proto converts an Org to its protobuf representation.
func (o *Org) Proto() *xagentv1.Org {
	return &xagentv1.Org{
		Id:        o.ID,
		Name:      o.Name,
		Owner:     o.Owner,
		CreatedAt: timestamppb.New(o.CreatedAt),
		UpdatedAt: timestamppb.New(o.UpdatedAt),
	}
}

// OrgMember represents a user's membership in an organisation.
type OrgMember struct {
	OrgID     int64     `json:"org_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

// OrgMemberWithUser is an OrgMember with user profile info for display.
type OrgMemberWithUser struct {
	OrgMember
	Email string `json:"email"`
	Name  string `json:"name"`
}

// Proto converts an OrgMemberWithUser to its protobuf representation.
func (m *OrgMemberWithUser) Proto() *xagentv1.OrgMember {
	return &xagentv1.OrgMember{
		OrgId:     m.OrgID,
		UserId:    m.UserID,
		Email:     m.Email,
		Name:      m.Name,
		Role:      m.Role,
		CreatedAt: timestamppb.New(m.CreatedAt),
	}
}

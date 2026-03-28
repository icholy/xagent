package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Org represents an organisation.
type Org struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts an Org to its protobuf representation.
func (o *Org) Proto() *xagentv1.Org {
	return &xagentv1.Org{
		Id:        o.ID,
		Name:      o.Name,
		OwnerId:   o.OwnerID,
		CreatedAt: timestamppb.New(o.CreatedAt),
	}
}

// OrgMember represents a membership in an organisation.
type OrgMember struct {
	ID        int64     `json:"id"`
	OrgID     int64     `json:"org_id"`
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts an OrgMember to its protobuf representation.
func (m *OrgMember) Proto() *xagentv1.OrgMember {
	return &xagentv1.OrgMember{
		UserId:    m.UserID,
		Email:     m.Email,
		Name:      m.Name,
		CreatedAt: timestamppb.New(m.CreatedAt),
	}
}

// User represents a user record populated on login.
type User struct {
	ID           string    `json:"id"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
	DefaultOrgID int64     `json:"default_org_id"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

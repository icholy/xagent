package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// User represents a user in the system.
type User struct {
	ID        int64     `json:"id"`
	GoogleID  string    `json:"google_id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Picture   string    `json:"picture"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Proto converts a User to its protobuf representation.
func (u *User) Proto() *xagentv1.User {
	return &xagentv1.User{
		Id:        u.ID,
		GoogleId:  u.GoogleID,
		Email:     u.Email,
		Name:      u.Name,
		Picture:   u.Picture,
		CreatedAt: timestamppb.New(u.CreatedAt),
		UpdatedAt: timestamppb.New(u.UpdatedAt),
	}
}

// UserFromProto converts a protobuf User to a model User.
func UserFromProto(pb *xagentv1.User) *User {
	var createdAt, updatedAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	if pb.UpdatedAt != nil {
		updatedAt = pb.UpdatedAt.AsTime()
	}
	return &User{
		ID:        pb.Id,
		GoogleID:  pb.GoogleId,
		Email:     pb.Email,
		Name:      pb.Name,
		Picture:   pb.Picture,
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

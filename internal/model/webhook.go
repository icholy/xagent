package model

import (
	"time"

	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Webhook represents a registered webhook endpoint.
type Webhook struct {
	UUID      string    `json:"uuid"`
	Secret    string    `json:"secret"`
	Owner     string    `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
}

// Proto converts a Webhook to its protobuf representation.
func (w *Webhook) Proto() *xagentv1.Webhook {
	return &xagentv1.Webhook{
		Uuid:      w.UUID,
		CreatedAt: timestamppb.New(w.CreatedAt),
	}
}

// WebhookFromProto converts a protobuf Webhook to a model Webhook.
func WebhookFromProto(pb *xagentv1.Webhook) *Webhook {
	var createdAt time.Time
	if pb.CreatedAt != nil {
		createdAt = pb.CreatedAt.AsTime()
	}
	return &Webhook{
		UUID:      pb.Uuid,
		CreatedAt: createdAt,
	}
}

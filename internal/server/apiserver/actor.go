package apiserver

import (
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
)

// actorFromCaller builds a model.Actor describing the API caller. API-key
// callers are tagged ActorKindAPIKey so audit rendering can distinguish
// keyed automation from interactive users; everything else (cookie/app
// tokens) is a user.
func actorFromCaller(caller *apiauth.UserInfo) model.Actor {
	kind := model.ActorKindUser
	if caller.Type == apiauth.AuthTypeKey {
		kind = model.ActorKindAPIKey
	}
	return model.Actor{
		Kind: kind,
		Name: caller.AuditName(),
		ID:   caller.ID,
	}
}

package apiserver

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/apiauth"
	"github.com/icholy/xagent/internal/model"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/lib/pq"
)

func (s *Server) CreateOrg(ctx context.Context, req *xagentv1.CreateOrgRequest) (*xagentv1.CreateOrgResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if req.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("name is required"))
	}
	org := &model.Org{
		Name:  req.Name,
		Owner: caller.ID,
	}
	err := s.store.WithTx(ctx, nil, func(tx *sql.Tx) error {
		if err := s.store.CreateOrg(ctx, tx, org); err != nil {
			return err
		}
		if err := s.store.AddOrgMember(ctx, tx, &model.OrgMember{
			OrgID:  org.ID,
			UserID: caller.ID,
			Role:   "owner",
		}); err != nil {
			return err
		}
		return tx.Commit()
	})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org created", "id", org.ID, "name", org.Name, "owner", caller.ID)
	return &xagentv1.CreateOrgResponse{Org: org.Proto()}, nil
}

func (s *Server) ListOrgs(ctx context.Context, req *xagentv1.ListOrgsRequest) (*xagentv1.ListOrgsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	orgs, err := s.store.ListOrgsByMember(ctx, nil, caller.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListOrgsResponse{Orgs: model.ProtoMap(orgs)}, nil
}

func (s *Server) DeleteOrg(ctx context.Context, req *xagentv1.DeleteOrgRequest) (*xagentv1.DeleteOrgResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, req.Id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("org not found"))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can delete it"))
	}
	user, err := s.store.GetUser(ctx, nil, caller.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if user.DefaultOrgID == req.Id {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("cannot delete your default org"))
	}
	if err := s.store.ArchiveOrg(ctx, nil, req.Id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org archived", "id", req.Id, "owner", caller.ID)
	return &xagentv1.DeleteOrgResponse{}, nil
}

func (s *Server) AddOrgMember(ctx context.Context, req *xagentv1.AddOrgMemberRequest) (*xagentv1.AddOrgMemberResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can add members"))
	}
	if req.Email == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("email is required"))
	}
	user, err := s.store.GetUserByEmail(ctx, nil, req.Email)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no user found with email %q", req.Email))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	member := &model.OrgMember{
		OrgID:  caller.OrgID,
		UserID: user.ID,
		Role:   "member",
	}
	if err := s.store.AddOrgMember(ctx, nil, member); err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" {
			return nil, connect.NewError(connect.CodeAlreadyExists, fmt.Errorf("user %q is already a member of this org", req.Email))
		}
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org member added", "org_id", caller.OrgID, "user_id", user.ID, "email", req.Email)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "added", Type: "org_members"}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		Time:      time.Now(),
	})
	return &xagentv1.AddOrgMemberResponse{
		Member: &xagentv1.OrgMember{
			OrgId:  member.OrgID,
			UserId: member.UserID,
			Email:  user.Email,
			Name:   user.Name,
			Role:   member.Role,
		},
	}, nil
}

func (s *Server) RemoveOrgMember(ctx context.Context, req *xagentv1.RemoveOrgMemberRequest) (*xagentv1.RemoveOrgMemberResponse, error) {
	caller := apiauth.MustCaller(ctx)
	org, err := s.store.GetOrg(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if org.Owner != caller.ID {
		return nil, connect.NewError(connect.CodePermissionDenied, errors.New("only the org owner can remove members"))
	}
	if req.UserId == org.Owner {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("cannot remove the org owner"))
	}
	if err := s.store.RemoveOrgMember(ctx, nil, caller.OrgID, req.UserId); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("org member removed", "org_id", caller.OrgID, "user_id", req.UserId)
	s.publish(model.Notification{
		Type:      "change",
		Resources: []model.NotificationResource{{Action: "removed", Type: "org_members"}},
		OrgID:     caller.OrgID,
		UserID:    caller.ID,
		Time:      time.Now(),
	})
	return &xagentv1.RemoveOrgMemberResponse{}, nil
}

func (s *Server) ListOrgMembers(ctx context.Context, req *xagentv1.ListOrgMembersRequest) (*xagentv1.ListOrgMembersResponse, error) {
	caller := apiauth.MustCaller(ctx)
	members, err := s.store.ListOrgMembersWithUsers(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.ListOrgMembersResponse{Members: model.ProtoMap(members)}, nil
}

func (s *Server) GetOrgSettings(ctx context.Context, req *xagentv1.GetOrgSettingsRequest) (*xagentv1.GetOrgSettingsResponse, error) {
	caller := apiauth.MustCaller(ctx)
	resp := &xagentv1.GetOrgSettingsResponse{
		McpUrl: s.baseURL + "/mcp",
	}
	if s.atlassian != nil {
		secret, err := s.atlassian.GetWebhookSecret(ctx, caller.OrgID)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		resp.AtlassianWebhookSecret = secret
		resp.AtlassianWebhookUrl = s.atlassian.WebhookURL(caller.OrgID)
	}
	if s.github != nil {
		resp.GithubAppUrl = s.github.AppInstallURL()
	}
	return resp, nil
}

func (s *Server) GenerateAtlassianWebhookSecret(ctx context.Context, req *xagentv1.GenerateAtlassianWebhookSecretRequest) (*xagentv1.GenerateAtlassianWebhookSecretResponse, error) {
	caller := apiauth.MustCaller(ctx)
	secret, err := s.atlassian.GenerateWebhookSecret(ctx, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return &xagentv1.GenerateAtlassianWebhookSecretResponse{
		Secret:     secret,
		WebhookUrl: s.atlassian.WebhookURL(caller.OrgID),
	}, nil
}

func (s *Server) UnlinkGitHubAccount(ctx context.Context, req *xagentv1.UnlinkGitHubAccountRequest) (*xagentv1.UnlinkGitHubAccountResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if err := s.store.UnlinkGitHubAccount(ctx, nil, caller.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("github account unlinked", "owner", caller.ID)
	return &xagentv1.UnlinkGitHubAccountResponse{}, nil
}

func (s *Server) UnlinkAtlassianAccount(ctx context.Context, req *xagentv1.UnlinkAtlassianAccountRequest) (*xagentv1.UnlinkAtlassianAccountResponse, error) {
	caller := apiauth.MustCaller(ctx)
	if err := s.atlassian.UnlinkAccount(ctx, caller.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	s.log.Info("atlassian account unlinked", "owner", caller.ID)
	return &xagentv1.UnlinkAtlassianAccountResponse{}, nil
}

func (s *Server) GetRoutingRules(ctx context.Context, req *xagentv1.GetRoutingRulesRequest) (*xagentv1.GetRoutingRulesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	rules, err := s.store.GetOrgRoutingRules(ctx, nil, caller.OrgID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pb := make([]*xagentv1.RoutingRule, len(rules))
	for i := range rules {
		pb[i] = rules[i].Proto()
	}
	return &xagentv1.GetRoutingRulesResponse{Rules: pb}, nil
}

func (s *Server) SetRoutingRules(ctx context.Context, req *xagentv1.SetRoutingRulesRequest) (*xagentv1.SetRoutingRulesResponse, error) {
	caller := apiauth.MustCaller(ctx)
	rules := make([]model.RoutingRule, len(req.Rules))
	for i, r := range req.Rules {
		rules[i] = model.RoutingRuleFromProto(r)
	}
	if err := s.store.SetOrgRoutingRules(ctx, nil, caller.OrgID, rules); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	pb := make([]*xagentv1.RoutingRule, len(rules))
	for i := range rules {
		pb[i] = rules[i].Proto()
	}
	return &xagentv1.SetRoutingRulesResponse{Rules: pb}, nil
}

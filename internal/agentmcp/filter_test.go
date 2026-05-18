package agentmcp

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"github.com/icholy/xagent/internal/auth/agentauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"gotest.tools/v3/assert"
)

func TestAgentFilter_SubmitRunnerEvents_Forwarded(t *testing.T) {
	var forwarded *xagentv1.SubmitRunnerEventsRequest
	client := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			forwarded = req
			return &xagentv1.SubmitRunnerEventsResponse{}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{TaskID: 42})
	req := &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: 42, Event: "started"},
			{TaskId: 42, Event: "stopped"},
		},
	}

	resp, err := filter.SubmitRunnerEvents(ctx, req)
	assert.NilError(t, err)
	assert.Assert(t, resp != nil)
	assert.Equal(t, forwarded, req, "request should be forwarded unchanged")
	assert.Equal(t, len(client.SubmitRunnerEventsCalls()), 1)
}

func TestAgentFilter_SubmitRunnerEvents_MismatchedTaskID(t *testing.T) {
	client := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			t.Fatal("underlying client must not be called when task id is mismatched")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{TaskID: 42})
	req := &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: 99, Event: "started"},
		},
	}

	_, err := filter.SubmitRunnerEvents(ctx, req)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_SubmitRunnerEvents_BatchMismatchAllOrNothing(t *testing.T) {
	client := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			t.Fatal("underlying client must not be called when any event has a mismatched task id")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{TaskID: 42})
	req := &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: 42, Event: "started"},
			{TaskId: 99, Event: "stopped"},
		},
	}

	_, err := filter.SubmitRunnerEvents(ctx, req)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_CreateGitHubToken_Forwarded(t *testing.T) {
	var forwarded *xagentv1.CreateGitHubTokenRequest
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			forwarded = req
			return &xagentv1.CreateGitHubTokenResponse{}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{TaskID: 42})
	req := &xagentv1.CreateGitHubTokenRequest{}

	resp, err := filter.CreateGitHubToken(ctx, req)
	assert.NilError(t, err)
	assert.Assert(t, resp != nil)
	assert.Equal(t, forwarded, req, "request should be forwarded unchanged")
	assert.Equal(t, len(client.CreateGitHubTokenCalls()), 1)
}

func TestAgentFilter_CreateGitHubToken_MissingClaims(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			t.Fatal("underlying client must not be called when claims are missing")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	req := &xagentv1.CreateGitHubTokenRequest{}

	_, err := filter.CreateGitHubToken(t.Context(), req)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_SubmitRunnerEvents_MissingClaims(t *testing.T) {
	client := &xagentclient.ClientMock{
		SubmitRunnerEventsFunc: func(ctx context.Context, req *xagentv1.SubmitRunnerEventsRequest) (*xagentv1.SubmitRunnerEventsResponse, error) {
			t.Fatal("underlying client must not be called when claims are missing")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	req := &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: 42, Event: "started"},
		},
	}

	_, err := filter.SubmitRunnerEvents(t.Context(), req)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

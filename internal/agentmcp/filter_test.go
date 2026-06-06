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

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
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

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
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

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	req := &xagentv1.SubmitRunnerEventsRequest{
		Events: []*xagentv1.RunnerEvent{
			{TaskId: 42, Event: "started"},
			{TaskId: 99, Event: "stopped"},
		},
	}

	_, err := filter.SubmitRunnerEvents(ctx, req)
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_CreateGitHubToken_Allowed(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			return &xagentv1.CreateGitHubTokenResponse{Token: "ghs_token"}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:       42,
			Workspace:    "test-workspace",
			Runner:       "test-runner",
			Capabilities: []string{agentauth.CapabilityGitHubToken},
		}),
	})
	resp, err := filter.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	assert.NilError(t, err)
	assert.Equal(t, resp.Token, "ghs_token")
	assert.Equal(t, len(client.CreateGitHubTokenCalls()), 1)
}

func TestAgentFilter_CreateGitHubToken_Denied(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateGitHubTokenFunc: func(ctx context.Context, req *xagentv1.CreateGitHubTokenRequest) (*xagentv1.CreateGitHubTokenResponse, error) {
			t.Fatal("underlying client must not be called when github token is disabled")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.CreateGitHubToken(ctx, &xagentv1.CreateGitHubTokenRequest{})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_ListChildTasks_Allowed(t *testing.T) {
	client := &xagentclient.ClientMock{
		ListChildTasksFunc: func(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
			return &xagentv1.ListChildTasksResponse{}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:       42,
			Workspace:    "test-workspace",
			Runner:       "test-runner",
			Capabilities: []string{agentauth.CapabilityChildTasks},
		}),
	})
	_, err := filter.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{ParentId: 42})
	assert.NilError(t, err)
	assert.Equal(t, len(client.ListChildTasksCalls()), 1)
}

func TestAgentFilter_ListChildTasks_Denied(t *testing.T) {
	client := &xagentclient.ClientMock{
		ListChildTasksFunc: func(ctx context.Context, req *xagentv1.ListChildTasksRequest) (*xagentv1.ListChildTasksResponse, error) {
			t.Fatal("underlying client must not be called when the child-tasks capability is absent")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.ListChildTasks(ctx, &xagentv1.ListChildTasksRequest{ParentId: 42})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_CreateTask_Denied(t *testing.T) {
	client := &xagentclient.ClientMock{
		CreateTaskFunc: func(ctx context.Context, req *xagentv1.CreateTaskRequest) (*xagentv1.CreateTaskResponse, error) {
			t.Fatal("underlying client must not be called when the child-tasks capability is absent")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.CreateTask(ctx, &xagentv1.CreateTaskRequest{
		Parent:    42,
		Workspace: "test-workspace",
		Runner:    "test-runner",
	})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_UpdateTask_OwnTaskAllowedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: 42}}, nil
		},
		UpdateTaskFunc: func(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
			return &xagentv1.UpdateTaskResponse{}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: 42})
	assert.NilError(t, err)
	assert.Equal(t, len(client.UpdateTaskCalls()), 1)
}

func TestAgentFilter_UpdateTask_ChildDeniedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: 99, Parent: 42}}, nil
		},
		UpdateTaskFunc: func(ctx context.Context, req *xagentv1.UpdateTaskRequest) (*xagentv1.UpdateTaskResponse, error) {
			t.Fatal("underlying client must not be called when the child-tasks capability is absent")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{Id: 99})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_GetTask_OwnTaskAllowedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: 42}}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.GetTask(ctx, &xagentv1.GetTaskRequest{Id: 42})
	assert.NilError(t, err)
}

func TestAgentFilter_GetTask_ChildDeniedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: 99, Parent: 42}}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.GetTask(ctx, &xagentv1.GetTaskRequest{Id: 99})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_ListLogs_OwnTaskAllowedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		ListLogsFunc: func(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
			return &xagentv1.ListLogsResponse{}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: 42})
	assert.NilError(t, err)
	assert.Equal(t, len(client.ListLogsCalls()), 1)
}

func TestAgentFilter_ListLogs_ChildDeniedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskFunc: func(ctx context.Context, req *xagentv1.GetTaskRequest) (*xagentv1.GetTaskResponse, error) {
			return &xagentv1.GetTaskResponse{Task: &xagentv1.Task{Id: 99, Parent: 42}}, nil
		},
		ListLogsFunc: func(ctx context.Context, req *xagentv1.ListLogsRequest) (*xagentv1.ListLogsResponse, error) {
			t.Fatal("underlying client must not be called when the child-tasks capability is absent")
			return nil, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.ListLogs(ctx, &xagentv1.ListLogsRequest{TaskId: 99})
	assert.Equal(t, connect.CodeOf(err), connect.CodePermissionDenied)
}

func TestAgentFilter_GetTaskDetails_OwnTaskAllowedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskDetailsFunc: func(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: 42}}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: 42})
	assert.NilError(t, err)
}

func TestAgentFilter_GetTaskDetails_ChildDeniedWithoutCapability(t *testing.T) {
	client := &xagentclient.ClientMock{
		GetTaskDetailsFunc: func(ctx context.Context, req *xagentv1.GetTaskDetailsRequest) (*xagentv1.GetTaskDetailsResponse, error) {
			return &xagentv1.GetTaskDetailsResponse{Task: &xagentv1.Task{Id: 99, Parent: 42}}, nil
		},
	}
	filter := NewAgentFilter(client)

	ctx := agentauth.ContextWithClaims(t.Context(), &agentauth.TaskClaims{
		TaskID: 42,
		Scopes: agentauth.Scopes(agentauth.ScopeOptions{
			TaskID:    42,
			Workspace: "test-workspace",
			Runner:    "test-runner",
		}),
	})
	_, err := filter.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{Id: 99})
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

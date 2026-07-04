package runner

import (
	"errors"
	"testing"

	"connectrpc.com/connect"
	"gotest.tools/v3/assert"
)

func TestIsPermanentError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"not found", connect.NewError(connect.CodeNotFound, errors.New("gone")), true},
		{"invalid argument", connect.NewError(connect.CodeInvalidArgument, errors.New("bad")), true},
		{"permission denied", connect.NewError(connect.CodePermissionDenied, errors.New("nope")), true},
		{"unavailable", connect.NewError(connect.CodeUnavailable, errors.New("down")), false},
		{"internal", connect.NewError(connect.CodeInternal, errors.New("boom")), false},
		{"deadline exceeded", connect.NewError(connect.CodeDeadlineExceeded, errors.New("slow")), false},
		{"plain error", errors.New("connection refused"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, isPermanentError(tt.err), tt.want)
		})
	}
}

package awsmicrovm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	v4 "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"gotest.tools/v3/assert"
)

func newTestClient(server *httptest.Server) *Client {
	return &Client{
		region:   "us-east-1",
		creds:    credentials.NewStaticCredentialsProvider("AKID", "secret", ""),
		signer:   v4.NewSigner(),
		endpoint: server.URL,
		http:     server.Client(),
		now:      func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) },
	}
}

func sigV4(auth string) bool { return strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKID/") }

func TestRunMicrovmSignsAndPosts(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody RunMicrovmInput
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(RunMicrovmOutput{MicrovmID: "mvm-1", Endpoint: "mvm-1.example.com"})
	}))
	defer srv.Close()

	out, err := newTestClient(srv).RunMicrovm(context.Background(), &RunMicrovmInput{
		ImageIdentifier:          "arn:image",
		ExecutionRoleArn:         "arn:role",
		RunHookPayload:           "opaque-payload",
		IdlePolicy:               &IdlePolicy{AutoResumeEnabled: false},
		IngressNetworkConnectors: []string{"arn:no-ingress"},
	})
	assert.NilError(t, err)
	assert.Equal(t, out.MicrovmID, "mvm-1")
	assert.Equal(t, gotPath, "/2025-09-09/microvms")
	assert.Assert(t, sigV4(gotAuth), "missing/invalid SigV4 auth header: %q", gotAuth)
	// The payload passes through verbatim.
	assert.Equal(t, gotBody.RunHookPayload, "opaque-payload")
}

func TestListAndTerminateAndGet(t *testing.T) {
	var terminateMethod, terminatePath, getPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/2025-09-09/microvms":
			_, _ = w.Write([]byte(`{"items":[{"microvmId":"mvm-1","state":"RUNNING"}]}`))
		case r.Method == http.MethodDelete:
			terminateMethod, terminatePath = r.Method, r.URL.Path
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/2025-09-09/microvms/"):
			getPath = r.URL.Path
			_, _ = w.Write([]byte(`{"microvmId":"mvm-1","state":"TERMINATED"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)

	list, err := c.ListMicrovms(context.Background(), &ListMicrovmsInput{})
	assert.NilError(t, err)
	assert.Equal(t, len(list.Microvms), 1)
	assert.Equal(t, list.Microvms[0].MicrovmID, "mvm-1")
	assert.Assert(t, list.Microvms[0].Alive())

	_, err = c.TerminateMicrovm(context.Background(), &TerminateMicrovmInput{MicrovmID: "mvm-1"})
	assert.NilError(t, err)
	assert.Equal(t, terminateMethod, http.MethodDelete)
	assert.Equal(t, terminatePath, "/2025-09-09/microvms/mvm-1")

	got, err := c.GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-1"})
	assert.NilError(t, err)
	assert.Equal(t, getPath, "/2025-09-09/microvms/mvm-1")
	assert.Assert(t, got.Microvm.State.Terminal())
}

func TestSuspendAndResume(t *testing.T) {
	var gotPath, gotMethod, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.SuspendMicrovm(context.Background(), &SuspendMicrovmInput{MicrovmID: "mvm-1"})
	assert.NilError(t, err)
	assert.Equal(t, gotPath, "/2025-09-09/microvms/mvm-1/suspend")
	assert.Equal(t, gotMethod, http.MethodPost)
	assert.Assert(t, sigV4(gotAuth), "missing/invalid SigV4 auth header: %q", gotAuth)

	_, err = c.ResumeMicrovm(context.Background(), &ResumeMicrovmInput{MicrovmID: "mvm-1"})
	assert.NilError(t, err)
	assert.Equal(t, gotPath, "/2025-09-09/microvms/mvm-1/resume")
	assert.Equal(t, gotMethod, http.MethodPost)
	assert.Assert(t, sigV4(gotAuth), "missing/invalid SigV4 auth header: %q", gotAuth)
}

func TestSuspendAndResumeAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"microvm mvm-x not found"}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	_, suspendErr := c.SuspendMicrovm(context.Background(), &SuspendMicrovmInput{MicrovmID: "mvm-x"})
	var suspendAPIErr *APIError
	assert.Assert(t, errors.As(suspendErr, &suspendAPIErr), "SuspendMicrovm error is not *APIError: %v", suspendErr)
	assert.Equal(t, suspendAPIErr.Op, "SuspendMicrovm")
	assert.Assert(t, IsNotFound(suspendErr))

	_, resumeErr := c.ResumeMicrovm(context.Background(), &ResumeMicrovmInput{MicrovmID: "mvm-x"})
	var resumeAPIErr *APIError
	assert.Assert(t, errors.As(resumeErr, &resumeAPIErr), "ResumeMicrovm error is not *APIError: %v", resumeErr)
	assert.Equal(t, resumeAPIErr.Op, "ResumeMicrovm")
	assert.Assert(t, IsNotFound(resumeErr))
}

func TestAPIErrorNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"microvm mvm-x not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	var apiErr *APIError
	assert.Assert(t, errors.As(err, &apiErr), "error is not *APIError: %v", err)
	assert.Equal(t, apiErr.StatusCode, http.StatusNotFound)
	assert.Equal(t, apiErr.Op, "GetMicrovm")
	assert.Equal(t, apiErr.Code, "ResourceNotFoundException")
	assert.Equal(t, apiErr.Message, "microvm mvm-x not found")
	assert.Assert(t, IsNotFound(err))
}

func TestIsNotFound(t *testing.T) {
	assert.Assert(t, !IsNotFound(nil))
	assert.Assert(t, !IsNotFound(errors.New("boom")))
	assert.Assert(t, !IsNotFound(&APIError{StatusCode: 500}))
	assert.Assert(t, IsNotFound(&APIError{StatusCode: 404}))

	// A 500 from the server is an *APIError but not a not-found.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"InternalFailure","message":"oops"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	assert.Assert(t, !IsNotFound(err), "IsNotFound should be false for a 500: %v", err)
	var apiErr *APIError
	assert.Assert(t, errors.As(err, &apiErr))
	assert.Equal(t, apiErr.Code, "InternalFailure")
	assert.Equal(t, apiErr.Message, "oops")
}

func TestAPIErrorUnparseableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).RunMicrovm(context.Background(), &RunMicrovmInput{ImageIdentifier: "arn:image"})
	var apiErr *APIError
	assert.Assert(t, errors.As(err, &apiErr), "error is not *APIError: %v", err)
	assert.Equal(t, apiErr.StatusCode, http.StatusBadGateway)
	assert.Equal(t, apiErr.Op, "RunMicrovm")
	assert.Equal(t, apiErr.Message, "<html>502 Bad Gateway</html>")
}

func TestAPIErrorEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	var apiErr *APIError
	assert.Assert(t, errors.As(err, &apiErr), "error is not *APIError: %v", err)
	assert.Equal(t, apiErr.StatusCode, http.StatusNotFound)
	assert.Equal(t, apiErr.Message, "")
	assert.Assert(t, IsNotFound(err))
}

func TestStatePredicates(t *testing.T) {
	assert.Assert(t, MicrovmStateTerminated.Terminal())
	assert.Assert(t, MicrovmStateTerminating.Terminal())
	assert.Assert(t, !MicrovmStatePending.Terminal())
	assert.Assert(t, !MicrovmStateRunning.Terminal())
	assert.Assert(t, !MicrovmStateSuspended.Terminal())
	assert.Assert(t, Microvm{State: MicrovmStateSuspended}.Alive())
}

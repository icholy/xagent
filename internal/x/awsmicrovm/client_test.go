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
		Tags:                     map[string]string{"k": "v"},
	})
	if err != nil {
		t.Fatalf("RunMicrovm: %v", err)
	}
	if out.MicrovmID != "mvm-1" {
		t.Fatalf("microvm id = %q", out.MicrovmID)
	}
	if gotPath != "/microvms" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("missing/invalid SigV4 auth header: %q", gotAuth)
	}
	// The payload and tags pass through verbatim.
	if gotBody.RunHookPayload != "opaque-payload" || gotBody.Tags["k"] != "v" {
		t.Fatalf("body = %+v", gotBody)
	}
}

func TestListAndTerminateAndGet(t *testing.T) {
	var terminatePath, getPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/microvms":
			_, _ = w.Write([]byte(`{"microvms":[{"microvmId":"mvm-1","state":"RUNNING","tags":{"k":"v"}}]}`))
		case strings.HasSuffix(r.URL.Path, "/terminate"):
			terminatePath = r.URL.Path
			w.WriteHeader(http.StatusOK)
		case strings.HasPrefix(r.URL.Path, "/microvms/"):
			getPath = r.URL.Path
			_, _ = w.Write([]byte(`{"microvmId":"mvm-1","state":"TERMINATED"}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := newTestClient(srv)

	list, err := c.ListMicrovms(context.Background(), &ListMicrovmsInput{})
	if err != nil {
		t.Fatalf("ListMicrovms: %v", err)
	}
	if len(list.Microvms) != 1 || list.Microvms[0].MicrovmID != "mvm-1" || !list.Microvms[0].Alive() {
		t.Fatalf("microvms = %+v", list.Microvms)
	}

	if _, err := c.TerminateMicrovm(context.Background(), &TerminateMicrovmInput{MicrovmID: "mvm-1"}); err != nil {
		t.Fatalf("TerminateMicrovm: %v", err)
	}
	if terminatePath != "/microvms/mvm-1/terminate" {
		t.Fatalf("terminate path = %q", terminatePath)
	}

	got, err := c.GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-1"})
	if err != nil {
		t.Fatalf("GetMicrovm: %v", err)
	}
	if getPath != "/microvms/mvm-1" || !got.Microvm.State.Terminal() {
		t.Fatalf("get path=%q state=%v", getPath, got.Microvm.State)
	}
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

	if _, err := c.SuspendMicrovm(context.Background(), &SuspendMicrovmInput{MicrovmID: "mvm-1"}); err != nil {
		t.Fatalf("SuspendMicrovm: %v", err)
	}
	if gotPath != "/microvms/mvm-1/suspend" {
		t.Fatalf("suspend path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("suspend method = %q", gotMethod)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("missing/invalid SigV4 auth header: %q", gotAuth)
	}

	if _, err := c.ResumeMicrovm(context.Background(), &ResumeMicrovmInput{MicrovmID: "mvm-1"}); err != nil {
		t.Fatalf("ResumeMicrovm: %v", err)
	}
	if gotPath != "/microvms/mvm-1/resume" {
		t.Fatalf("resume path = %q", gotPath)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("resume method = %q", gotMethod)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("missing/invalid SigV4 auth header: %q", gotAuth)
	}
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
	if !errors.As(suspendErr, &suspendAPIErr) {
		t.Fatalf("SuspendMicrovm error is not *APIError: %v", suspendErr)
	}
	if suspendAPIErr.Op != "SuspendMicrovm" || !IsNotFound(suspendErr) {
		t.Fatalf("suspend api error = %+v", suspendAPIErr)
	}

	_, resumeErr := c.ResumeMicrovm(context.Background(), &ResumeMicrovmInput{MicrovmID: "mvm-x"})
	var resumeAPIErr *APIError
	if !errors.As(resumeErr, &resumeAPIErr) {
		t.Fatalf("ResumeMicrovm error is not *APIError: %v", resumeErr)
	}
	if resumeAPIErr.Op != "ResumeMicrovm" || !IsNotFound(resumeErr) {
		t.Fatalf("resume api error = %+v", resumeAPIErr)
	}
}

func TestAPIErrorNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"microvm mvm-x not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	if err == nil {
		t.Fatal("expected error")
	}

	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if apiErr.Op != "GetMicrovm" {
		t.Fatalf("op = %q", apiErr.Op)
	}
	if apiErr.Code != "ResourceNotFoundException" {
		t.Fatalf("code = %q", apiErr.Code)
	}
	if apiErr.Message != "microvm mvm-x not found" {
		t.Fatalf("message = %q", apiErr.Message)
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound should be true for a 404")
	}
}

func TestIsNotFound(t *testing.T) {
	if IsNotFound(nil) {
		t.Fatal("IsNotFound(nil) should be false")
	}
	if IsNotFound(errors.New("boom")) {
		t.Fatal("IsNotFound(non-APIError) should be false")
	}
	if IsNotFound(&APIError{StatusCode: 500}) {
		t.Fatal("IsNotFound(500) should be false")
	}
	if !IsNotFound(&APIError{StatusCode: 404}) {
		t.Fatal("IsNotFound(404) should be true")
	}

	// A 500 from the server is an *APIError but not a not-found.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"code":"InternalFailure","message":"oops"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	if IsNotFound(err) {
		t.Fatalf("IsNotFound should be false for a 500: %v", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Code != "InternalFailure" || apiErr.Message != "oops" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAPIErrorUnparseableBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>502 Bad Gateway</html>"))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).RunMicrovm(context.Background(), &RunMicrovmInput{ImageIdentifier: "arn:image"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("status = %d", apiErr.StatusCode)
	}
	if apiErr.Op != "RunMicrovm" {
		t.Fatalf("op = %q", apiErr.Op)
	}
	if apiErr.Message != "<html>502 Bad Gateway</html>" {
		t.Fatalf("message = %q", apiErr.Message)
	}
}

func TestAPIErrorEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := newTestClient(srv).GetMicrovm(context.Background(), &GetMicrovmInput{MicrovmID: "mvm-x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.StatusCode != http.StatusNotFound || apiErr.Message != "" {
		t.Fatalf("status=%d message=%q", apiErr.StatusCode, apiErr.Message)
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound should be true even with empty body")
	}
}

func TestStatePredicates(t *testing.T) {
	if !MicrovmStateTerminated.Terminal() || !MicrovmStateFailed.Terminal() {
		t.Fatal("terminated/failed should be terminal")
	}
	if MicrovmStateRunning.Terminal() || MicrovmStateSuspended.Terminal() {
		t.Fatal("running/suspended should not be terminal")
	}
	if !(Microvm{State: MicrovmStateSuspended}).Alive() {
		t.Fatal("suspended should be alive")
	}
}

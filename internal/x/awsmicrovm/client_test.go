package awsmicrovm

import (
	"context"
	"encoding/json"
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

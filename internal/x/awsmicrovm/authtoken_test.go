package awsmicrovm

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateMicrovmAuthTokenSignsAndPosts(t *testing.T) {
	var gotPath, gotAuth, gotMethod string
	var gotBody struct {
		MicrovmID         string          `json:"microvmIdentifier"`
		ExpirationMinutes int             `json:"expirationInMinutes"`
		AllowedPorts      json.RawMessage `json:"allowedPorts"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"authToken":{"X-aws-proxy-auth":"tok-123"}}`))
	}))
	defer srv.Close()

	out, err := newTestClient(srv).CreateMicrovmAuthToken(context.Background(), &CreateMicrovmAuthTokenInput{
		MicrovmID:         "mvm-1",
		ExpirationMinutes: 30,
		AllowedPorts:      []AllowedPort{AllPorts()},
	})
	if err != nil {
		t.Fatalf("CreateMicrovmAuthToken: %v", err)
	}
	if out.Token != "tok-123" {
		t.Fatalf("token = %q", out.Token)
	}
	if gotMethod != http.MethodPost {
		t.Fatalf("method = %q", gotMethod)
	}
	if gotPath != "/microvms/mvm-1/auth-tokens" {
		t.Fatalf("path = %q", gotPath)
	}
	if !strings.HasPrefix(gotAuth, "AWS4-HMAC-SHA256 Credential=AKID/") {
		t.Fatalf("missing/invalid SigV4 auth header: %q", gotAuth)
	}
	if gotBody.MicrovmID != "mvm-1" || gotBody.ExpirationMinutes != 30 {
		t.Fatalf("body = %+v", gotBody)
	}
	if string(gotBody.AllowedPorts) != `[{"allPorts":{}}]` {
		t.Fatalf("allowedPorts = %s", gotBody.AllowedPorts)
	}
}

func TestCreateMicrovmAuthTokenAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"__type":"ResourceNotFoundException","message":"microvm mvm-x not found"}`))
	}))
	defer srv.Close()

	_, err := newTestClient(srv).CreateMicrovmAuthToken(context.Background(), &CreateMicrovmAuthTokenInput{MicrovmID: "mvm-x"})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not *APIError: %v", err)
	}
	if apiErr.Op != "CreateMicrovmAuthToken" || apiErr.StatusCode != http.StatusNotFound {
		t.Fatalf("op=%q status=%d", apiErr.Op, apiErr.StatusCode)
	}
	if !IsNotFound(err) {
		t.Fatal("IsNotFound should be true for a 404")
	}
}

func TestAllowedPortRoundTrip(t *testing.T) {
	in := []AllowedPort{AllPorts(), Port(8080)}
	data, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(data) != `[{"allPorts":{}},{"port":8080}]` {
		t.Fatalf("marshal = %s", data)
	}

	var out []AllowedPort
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 2 || !out[0].IsAllPorts() {
		t.Fatalf("out = %+v", out)
	}
	if n, ok := out[1].PortNumber(); !ok || n != 8080 {
		t.Fatalf("port = %d ok=%v", n, ok)
	}
	if _, ok := out[0].PortNumber(); ok {
		t.Fatal("all-ports element should report no port number")
	}
}

func TestAllowedPortUnmarshalUnknown(t *testing.T) {
	var p AllowedPort
	if err := p.UnmarshalJSON([]byte(`{"somethingElse":{}}`)); err == nil {
		t.Fatal("expected error for unrecognized element")
	}
}

func TestNewProxyRequestSetsURLAndHeader(t *testing.T) {
	req, err := NewProxyRequest(context.Background(), "mvm-1.example.com", "tok-123", http.MethodGet, "/health", nil)
	if err != nil {
		t.Fatalf("NewProxyRequest: %v", err)
	}
	if req.URL.String() != "https://mvm-1.example.com/health" {
		t.Fatalf("url = %q", req.URL.String())
	}
	if req.Header.Get(ProxyAuthHeader) != "tok-123" {
		t.Fatalf("header = %q", req.Header.Get(ProxyAuthHeader))
	}

	// An endpoint that already carries a scheme is used as-is.
	req, err = NewProxyRequest(context.Background(), "https://mvm-1.example.com/", "tok", http.MethodPost, "/run", nil)
	if err != nil {
		t.Fatalf("NewProxyRequest: %v", err)
	}
	if req.URL.String() != "https://mvm-1.example.com/run" {
		t.Fatalf("url = %q", req.URL.String())
	}
}

func TestNewProxyRequestStreamsSSE(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(ProxyAuthHeader) != "tok-123" {
			t.Errorf("missing proxy auth header: %q", r.Header.Get(ProxyAuthHeader))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, line := range []string{"data: one\n\n", "data: two\n\n"} {
			_, _ = io.WriteString(w, line)
			if fl != nil {
				fl.Flush()
			}
		}
	}))
	defer srv.Close()

	req, err := NewProxyRequest(context.Background(), srv.URL, "tok-123", http.MethodGet, "/events", nil)
	if err != nil {
		t.Fatalf("NewProxyRequest: %v", err)
	}
	req.Header.Set("Accept", "text/event-stream")

	// The caller sends the request with its own client and reads the streamed
	// body — the helper does not parse it.
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	var events []string
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		if line := sc.Text(); strings.HasPrefix(line, "data: ") {
			events = append(events, strings.TrimPrefix(line, "data: "))
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(events) != 2 || events[0] != "one" || events[1] != "two" {
		t.Fatalf("events = %v", events)
	}
}

package awsmicrovm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, h http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHandlerRoutesAndDispatches(t *testing.T) {
	var ranPayload, ranID, terminatedID string
	h := &Handler{
		Run: func(_ context.Context, req RunHookRequest) error {
			ranPayload = req.Payload
			ranID = req.MicrovmID
			return nil
		},
		Terminate: func(_ context.Context, req TerminateHookRequest) error {
			terminatedID = req.MicrovmID
			return nil
		},
	}

	if rec := post(t, h, HookRun, `{"microvmId":"mvm-1","runHookPayload":"opaque"}`); rec.Code != http.StatusOK {
		t.Fatalf("run = %d", rec.Code)
	}
	if ranPayload != "opaque" || ranID != "mvm-1" {
		t.Fatalf("run dispatch: payload=%q id=%q", ranPayload, ranID)
	}

	if rec := post(t, h, HookTerminate, `{"microvmId":"mvm-1"}`); rec.Code != http.StatusOK {
		t.Fatalf("terminate = %d", rec.Code)
	}
	if terminatedID != "mvm-1" {
		t.Fatalf("terminate dispatch: id=%q", terminatedID)
	}
}

func TestHandlerNilFieldsDefaultTo200(t *testing.T) {
	h := &Handler{} // no hooks set
	for _, path := range []string{HookRun, HookTerminate, HookSuspend, HookResume} {
		if rec := post(t, h, path, ""); rec.Code != http.StatusOK {
			t.Fatalf("%s with nil hook = %d; want 200", path, rec.Code)
		}
	}
}

func TestHandlerHookErrorIs500(t *testing.T) {
	h := &Handler{Run: func(context.Context, RunHookRequest) error { return context.Canceled }}
	if rec := post(t, h, HookRun, `{"microvmId":"x"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("run error = %d; want 500", rec.Code)
	}
}

func TestHandlerBadBodyIs400(t *testing.T) {
	h := &Handler{Run: func(context.Context, RunHookRequest) error { return nil }}
	if rec := post(t, h, HookRun, `{not json`); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body = %d; want 400", rec.Code)
	}
}

func TestHandlerUnknownPathIs404(t *testing.T) {
	h := &Handler{}
	if rec := post(t, h, "/aws/lambda-microvms/runtime/v1/bogus", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown path = %d; want 404", rec.Code)
	}
}

package awsmicrovm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
)

// Lifecycle hook paths Lambda calls on the application (served on the dedicated
// HookPort). The application exposes these so Lambda can drive the MicroVM
// lifecycle.
const (
	HookBase      = "/aws/lambda-microvms/runtime/v1"
	HookRun       = HookBase + "/run"
	HookSuspend   = HookBase + "/suspend"
	HookResume    = HookBase + "/resume"
	HookTerminate = HookBase + "/terminate"

	// DefaultPort is the port Lambda routes MicroVM ingress to by default. The
	// xagent control surface (/xagent/lifecycle + /xagent/stop) is served here so
	// the runner can reach it over the managed proxy.
	DefaultPort = 8080

	// HookPort is the dedicated port the AWS lifecycle hooks are served on. It is
	// control-plane-internal (Lambda → shim, NOT reached over the ingress proxy)
	// and MUST match the port declared to create-microvm-image via `--hooks
	// port=...`. Lambda cannot reach the hooks on the ingress port, so they get
	// their own port here.
	HookPort = 9000
)

// RunHookRequest is delivered to the /run hook after a MicroVM starts. Payload
// is the run-hook payload string (≤16 KB) passed to RunMicrovm, verbatim — its
// meaning is the caller's concern, never this package's.
type RunHookRequest struct {
	MicrovmID string
	Payload   string
}

// TerminateHookRequest is delivered to the /terminate hook before the MicroVM's
// resources are released.
type TerminateHookRequest struct {
	MicrovmID string
}

// SuspendHookRequest is delivered to the /suspend hook before the MicroVM
// suspends.
type SuspendHookRequest struct {
	MicrovmID string
}

// ResumeHookRequest is delivered to the /resume hook when a suspended MicroVM
// resumes.
type ResumeHookRequest struct {
	MicrovmID string
}

// Handler is the http.Handler an application exposes to receive MicroVM
// lifecycle hooks. It owns routing, the JSON wire format, and status codes; the
// consumer owns behavior by setting the func fields. A nil field responds 200
// (the default "acknowledged" reply). A non-nil hook that returns an error
// responds 500.
type Handler struct {
	Run       func(ctx context.Context, req RunHookRequest) error
	Terminate func(ctx context.Context, req TerminateHookRequest) error
	Suspend   func(ctx context.Context, req SuspendHookRequest) error
	Resume    func(ctx context.Context, req ResumeHookRequest) error
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case HookRun:
		h.serve(w, r, func(body hookBody) error {
			if h.Run == nil {
				return nil
			}
			return h.Run(r.Context(), RunHookRequest{MicrovmID: body.MicrovmID, Payload: body.RunHookPayload})
		})
	case HookTerminate:
		h.serve(w, r, func(body hookBody) error {
			if h.Terminate == nil {
				return nil
			}
			return h.Terminate(r.Context(), TerminateHookRequest{MicrovmID: body.MicrovmID})
		})
	case HookSuspend:
		h.serve(w, r, func(body hookBody) error {
			if h.Suspend == nil {
				return nil
			}
			return h.Suspend(r.Context(), SuspendHookRequest{MicrovmID: body.MicrovmID})
		})
	case HookResume:
		h.serve(w, r, func(body hookBody) error {
			if h.Resume == nil {
				return nil
			}
			return h.Resume(r.Context(), ResumeHookRequest{MicrovmID: body.MicrovmID})
		})
	default:
		http.NotFound(w, r)
	}
}

// hookBody is the union of fields Lambda includes in hook request bodies. The
// /run hook carries the run-hook payload; all hooks may carry the MicroVM id.
type hookBody struct {
	MicrovmID      string `json:"microvmId"`
	RunHookPayload string `json:"runHookPayload"`
}

func (h *Handler) serve(w http.ResponseWriter, r *http.Request, dispatch func(hookBody) error) {
	var body hookBody
	if data, _ := io.ReadAll(r.Body); len(data) > 0 {
		if err := json.Unmarshal(data, &body); err != nil {
			http.Error(w, "invalid hook body", http.StatusBadRequest)
			return
		}
	}
	if err := dispatch(body); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
}

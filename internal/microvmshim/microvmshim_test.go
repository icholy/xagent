package microvmshim

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/runner/backend/lambdamicrovm"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
)

// fakeProcess is a controllable Process for tests.
type fakeProcess struct {
	mu       sync.Mutex
	exit     chan struct{}
	signals  []os.Signal
	killed   bool
	exitOnce sync.Once
}

func newFakeProcess() *fakeProcess { return &fakeProcess{exit: make(chan struct{})} }

func (p *fakeProcess) Wait() error {
	<-p.exit
	return nil
}

func (p *fakeProcess) Signal(s os.Signal) error {
	p.mu.Lock()
	p.signals = append(p.signals, s)
	p.mu.Unlock()
	return nil
}

func (p *fakeProcess) Kill() error {
	p.mu.Lock()
	p.killed = true
	p.mu.Unlock()
	p.finish()
	return nil
}

func (p *fakeProcess) finish() { p.exitOnce.Do(func() { close(p.exit) }) }

func (p *fakeProcess) gotSignal(s os.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, got := range p.signals {
		if got == s {
			return true
		}
	}
	return false
}

func bundleJSON(t *testing.T, b lambdamicrovm.Bundle) []byte {
	t.Helper()
	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func postRun(t *testing.T, h http.Handler, microvmID, payload string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"microvmId": microvmID, "runHookPayload": payload})
	req := httptest.NewRequest(http.MethodPost, awsmicrovm.HookRun, strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRunFetchesProvisionsSpawnsAndSelfTerminates(t *testing.T) {
	proc := newFakeProcess()
	var provisioned lambdamicrovm.Bundle
	terminated := make(chan string, 1)

	s := &Server{
		Fetch: func(_ context.Context, url string) ([]byte, error) {
			if url != "https://staging/bundle" {
				t.Errorf("unexpected fetch url %q", url)
			}
			return bundleJSON(t, lambdamicrovm.Bundle{Cmd: []string{"/usr/local/bin/xagent", "driver"}}), nil
		},
		Provision: func(b lambdamicrovm.Bundle) error { provisioned = b; return nil },
		Start:     func(_ context.Context, _ lambdamicrovm.Bundle) (Process, error) { return proc, nil },
		Terminate: func(_ context.Context, id string) error { terminated <- id; return nil },
	}
	h := s.Handler()

	rec := postRun(t, h, "mvm-abc", "https://staging/bundle")
	if rec.Code != http.StatusOK {
		t.Fatalf("run returned %d", rec.Code)
	}
	if len(provisioned.Cmd) == 0 {
		t.Fatal("bundle was not provisioned")
	}

	// Driver exits -> shim self-terminates the VM with its id.
	proc.finish()
	select {
	case id := <-terminated:
		if id != "mvm-abc" {
			t.Fatalf("self-terminated %q; want mvm-abc", id)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("shim did not self-terminate after driver exit")
	}
}

func TestRunIsIdempotent(t *testing.T) {
	proc := newFakeProcess()
	var starts int
	s := &Server{
		Fetch: func(_ context.Context, _ string) ([]byte, error) {
			return bundleJSON(t, lambdamicrovm.Bundle{Cmd: []string{"x"}}), nil
		},
		Provision: func(lambdamicrovm.Bundle) error { return nil },
		Start: func(_ context.Context, _ lambdamicrovm.Bundle) (Process, error) {
			starts++
			return proc, nil
		},
		Terminate: func(_ context.Context, _ string) error { return nil },
	}
	h := s.Handler()

	postRun(t, h, "mvm-1", "u")
	postRun(t, h, "mvm-1", "u") // resume / duplicate /run
	if starts != 1 {
		t.Fatalf("driver started %d times; want 1", starts)
	}
}

func TestTerminateHookSignalsDriver(t *testing.T) {
	proc := newFakeProcess()
	s := &Server{
		Fetch: func(_ context.Context, _ string) ([]byte, error) {
			return bundleJSON(t, lambdamicrovm.Bundle{Cmd: []string{"x"}}), nil
		},
		Provision: func(lambdamicrovm.Bundle) error { return nil },
		Start:     func(_ context.Context, _ lambdamicrovm.Bundle) (Process, error) { return proc, nil },
		Terminate: func(_ context.Context, _ string) error { return nil },
		Grace:     time.Second,
	}
	h := s.Handler()
	postRun(t, h, "mvm-1", "u")

	// Drive the terminate hook; the driver exits promptly on SIGTERM.
	go func() {
		time.Sleep(20 * time.Millisecond)
		proc.finish()
	}()
	req := httptest.NewRequest(http.MethodPost, awsmicrovm.HookTerminate, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("terminate returned %d", rec.Code)
	}
	if !proc.gotSignal(syscall.SIGTERM) {
		t.Fatal("driver was not sent SIGTERM")
	}
	if proc.killed {
		t.Fatal("driver should not be killed when it exits within grace")
	}
}

func TestTerminateHookEscalatesToKill(t *testing.T) {
	proc := newFakeProcess()
	s := &Server{
		Fetch: func(_ context.Context, _ string) ([]byte, error) {
			return bundleJSON(t, lambdamicrovm.Bundle{Cmd: []string{"x"}}), nil
		},
		Provision: func(lambdamicrovm.Bundle) error { return nil },
		Start:     func(_ context.Context, _ lambdamicrovm.Bundle) (Process, error) { return proc, nil },
		Terminate: func(_ context.Context, _ string) error { return nil },
		Grace:     10 * time.Millisecond, // driver never exits on its own
	}
	h := s.Handler()
	postRun(t, h, "mvm-1", "u")

	req := httptest.NewRequest(http.MethodPost, awsmicrovm.HookTerminate, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if !proc.killed {
		t.Fatal("driver should be SIGKILLed after the grace period")
	}
}

func TestSuspendResumeAre200(t *testing.T) {
	s := &Server{}
	h := s.Handler()
	for _, path := range []string{awsmicrovm.HookSuspend, awsmicrovm.HookResume} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s returned %d", path, rec.Code)
		}
	}
}

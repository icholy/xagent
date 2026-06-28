package lambdamicrovm

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/icholy/xagent/internal/runner/backend"
	"github.com/icholy/xagent/internal/runner/workspace"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
)

// fakeCloud is an in-memory Cloud for tests.
type fakeCloud struct {
	mu      sync.Mutex
	next    int
	vms     map[string]*awsmicrovm.Microvm
	lastRun *awsmicrovm.RunMicrovmInput
	runErr  error
	// pageSize > 0 makes ListMicrovms paginate (NextToken is the start index),
	// exercising the backend's listAll paginator.
	pageSize int
}

func newFakeCloud() *fakeCloud { return &fakeCloud{vms: map[string]*awsmicrovm.Microvm{}} }

func (f *fakeCloud) RunMicrovm(_ context.Context, in *awsmicrovm.RunMicrovmInput) (*awsmicrovm.RunMicrovmOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.runErr != nil {
		return nil, f.runErr
	}
	f.lastRun = in
	f.next++
	id := fmt.Sprintf("mvm-%d", f.next)
	f.vms[id] = &awsmicrovm.Microvm{MicrovmID: id, State: awsmicrovm.MicrovmStateRunning, Tags: in.Tags}
	return &awsmicrovm.RunMicrovmOutput{MicrovmID: id, Endpoint: id + ".example.com"}, nil
}

func (f *fakeCloud) TerminateMicrovm(_ context.Context, in *awsmicrovm.TerminateMicrovmInput) (*awsmicrovm.TerminateMicrovmOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if vm, ok := f.vms[in.MicrovmID]; ok {
		vm.State = awsmicrovm.MicrovmStateTerminated
	}
	return &awsmicrovm.TerminateMicrovmOutput{}, nil
}

func (f *fakeCloud) ListMicrovms(_ context.Context, in *awsmicrovm.ListMicrovmsInput) (*awsmicrovm.ListMicrovmsOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// Stable order so NextToken (a start index) paginates deterministically.
	ids := make([]string, 0, len(f.vms))
	for id := range f.vms {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	start := 0
	if in != nil && in.NextToken != "" {
		start, _ = strconv.Atoi(in.NextToken)
	}
	size := f.pageSize
	if size <= 0 {
		size = len(ids)
	}
	end := start + size
	if end > len(ids) {
		end = len(ids)
	}
	out := &awsmicrovm.ListMicrovmsOutput{}
	for _, id := range ids[start:end] {
		out.Microvms = append(out.Microvms, *f.vms[id])
	}
	if end < len(ids) {
		out.NextToken = strconv.Itoa(end)
	}
	return out, nil
}

func (f *fakeCloud) setState(id string, s awsmicrovm.MicrovmState) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if vm, ok := f.vms[id]; ok {
		vm.State = s
	}
}

func (f *fakeCloud) drop(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.vms, id)
}

// fakeStager is an in-memory Stager.
type fakeStager struct {
	mu      sync.Mutex
	objects map[string][]byte
}

func newFakeStager() *fakeStager { return &fakeStager{objects: map[string][]byte{}} }

func (f *fakeStager) Stage(_ context.Context, bucket, key string, data []byte, _ int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objects[key] = data
	return "https://" + bucket + ".staging.example.com/" + key, nil
}

func (f *fakeStager) Remove(_ context.Context, _, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.objects, key)
	return nil
}

func newTestBackend(t *testing.T) (*Backend, *fakeCloud, *fakeStager) {
	t.Helper()
	cloud := newFakeCloud()
	stager := newFakeStager()
	be, err := New(Options{Cloud: cloud, Stager: stager, RunnerID: "runner-1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return be, cloud, stager
}

func testWorkspace() *workspace.Workspace {
	return &workspace.Workspace{
		LambdaMicroVM: &workspace.LambdaMicroVM{
			ImageIdentifier: "arn:aws:lambda:us-east-1:123:microvm-image/x",
			ExecutionRole:   "arn:aws:iam::123:role/x",
			EgressConnector: "arn:aws:lambda:us-east-1:aws:network-connector:aws-network-connector:INTERNET_EGRESS",
			StagingBucket:   "bucket",
			Environment:     map[string]string{"FOO": "bar"},
		},
	}
}

func testSpec(taskID int64) *backend.Spec {
	return &backend.Spec{
		TaskID:    taskID,
		Workspace: testWorkspace(),
		Cmd:       []string{backend.BinaryPath, "driver", "--task", "1"},
		Env:       []string{"XAGENT_TASK_ID=1"},
		Files: []backend.File{
			{Path: "/tmp/xagent", Mode: 0777, Dir: true},
			{Path: "/tmp/xagent/1.json", Data: []byte(`{"type":"claude"}`), Mode: 0666},
		},
	}
}

func TestValidateWorkspace(t *testing.T) {
	be, _, _ := newTestBackend(t)
	if err := be.ValidateWorkspace(testWorkspace()); err != nil {
		t.Fatalf("valid workspace rejected: %v", err)
	}
	cases := map[string]func(*workspace.LambdaMicroVM){
		"missing image":    func(c *workspace.LambdaMicroVM) { c.ImageIdentifier = "" },
		"missing role":     func(c *workspace.LambdaMicroVM) { c.ExecutionRole = "" },
		"missing egress":   func(c *workspace.LambdaMicroVM) { c.EgressConnector = "" },
		"missing bucket":   func(c *workspace.LambdaMicroVM) { c.StagingBucket = "" },
		"duration too big": func(c *workspace.LambdaMicroVM) { c.MaxDurationSeconds = 99999 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ws := testWorkspace()
			mutate(ws.LambdaMicroVM)
			if err := be.ValidateWorkspace(ws); err == nil {
				t.Fatalf("expected error for %s", name)
			}
		})
	}
	t.Run("nil config", func(t *testing.T) {
		if err := be.ValidateWorkspace(&workspace.Workspace{}); err == nil {
			t.Fatal("expected error for nil lambda_microvm")
		}
	})
}

func TestLaunchStagesBundleAndReturnsHandle(t *testing.T) {
	be, cloud, stager := newTestBackend(t)
	ctx := context.Background()

	h, err := be.Launch(ctx, testSpec(7), nil)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	// The handle identifies the MicroVM and carries the staging location.
	if h.Type != HandleType || h.ID != "mvm-1" {
		t.Fatalf("handle = %+v", h)
	}
	var hd handleData
	if err := json.Unmarshal(h.Data, &hd); err != nil {
		t.Fatalf("handle data: %v", err)
	}
	if hd.StageBucket != "bucket" || hd.StageKey != "runner-1/7.json" || hd.ImageARN == "" {
		t.Fatalf("handle data = %+v", hd)
	}

	// The MicroVM was launched with the workspace's run parameters and tags.
	if cloud.lastRun.MaximumDurationInSeconds != defaultMaxDuration {
		t.Fatalf("max duration = %d", cloud.lastRun.MaximumDurationInSeconds)
	}
	if cloud.lastRun.Tags[tagTask] != "7" || cloud.lastRun.Tags[tagRunner] != "runner-1" {
		t.Fatalf("tags = %v", cloud.lastRun.Tags)
	}
	if cloud.lastRun.RunHookPayload == "" {
		t.Fatal("run-hook payload (staged URL) is empty")
	}

	// The bundle was staged (not inlined in the payload) and merges workspace env.
	raw, ok := stager.objects["runner-1/7.json"]
	if !ok {
		t.Fatal("bundle not staged under expected key")
	}
	var bundle Bundle
	if err := json.Unmarshal(raw, &bundle); err != nil {
		t.Fatalf("staged bundle invalid: %v", err)
	}
	if len(bundle.Files) != 2 || bundle.Env[0] != "FOO=bar" {
		t.Fatalf("bundle = %+v", bundle)
	}

	if state, err := be.Probe(ctx, h); err != nil || state != backend.StateRunning {
		t.Fatalf("Probe = %v, %v; want running", state, err)
	}
}

func TestLaunchWithReuseCleansStaleObject(t *testing.T) {
	be, _, stager := newTestBackend(t)
	ctx := context.Background()

	h1, err := be.Launch(ctx, testSpec(1), nil)
	if err != nil {
		t.Fatalf("first Launch: %v", err)
	}
	if _, ok := stager.objects["runner-1/1.json"]; !ok {
		t.Fatal("first bundle not staged")
	}

	// Relaunch with the prior handle as reuse: its stale staged object is removed
	// before the fresh one is staged (same key here, so just assert a fresh VM).
	h2, err := be.Launch(ctx, testSpec(1), &h1)
	if err != nil {
		t.Fatalf("relaunch: %v", err)
	}
	if h2.ID == h1.ID {
		t.Fatalf("relaunch should produce a fresh MicroVM, got %s", h2.ID)
	}
	if _, ok := stager.objects["runner-1/1.json"]; !ok {
		t.Fatal("fresh bundle should be staged")
	}
}

func TestProbeExitedAndAbsent(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	ctx := context.Background()
	h, _ := be.Launch(ctx, testSpec(1), nil)

	cloud.setState(h.ID, awsmicrovm.MicrovmStateFailed)
	if state, _ := be.Probe(ctx, h); state != backend.StateExited {
		t.Fatalf("failed VM Probe = %v; want exited", state)
	}

	cloud.drop(h.ID)
	if state, _ := be.Probe(ctx, h); state != backend.StateExited {
		t.Fatalf("absent VM Probe = %v; want exited", state)
	}
}

func TestSignalTerminates(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	ctx := context.Background()
	h, _ := be.Launch(ctx, testSpec(1), nil)

	signalled, err := be.Signal(ctx, h)
	if err != nil || !signalled {
		t.Fatalf("Signal = %v, %v; want signalled", signalled, err)
	}
	if cloud.vms[h.ID].State != awsmicrovm.MicrovmStateTerminated {
		t.Fatal("MicroVM was not terminated")
	}

	// Signalling an already-terminated handle signals nothing.
	signalled, err = be.Signal(ctx, h)
	if err != nil || signalled {
		t.Fatalf("Signal on terminated = %v, %v; want not signalled", signalled, err)
	}
}

func TestDestroyTerminatesAndCleansStaging(t *testing.T) {
	be, cloud, stager := newTestBackend(t)
	ctx := context.Background()
	h, _ := be.Launch(ctx, testSpec(5), nil)

	if err := be.Destroy(ctx, h); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if cloud.vms[h.ID].State != awsmicrovm.MicrovmStateTerminated {
		t.Fatal("Destroy should terminate a live MicroVM")
	}
	if _, ok := stager.objects["runner-1/5.json"]; ok {
		t.Fatal("Destroy should delete the staged bundle")
	}
	// Idempotent.
	if err := be.Destroy(ctx, h); err != nil {
		t.Fatalf("Destroy (repeat): %v", err)
	}
}

func TestWatchReportsExitOnce(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	be.poll = time.Millisecond

	h1, _ := be.Launch(ctx, testSpec(1), nil) // clean termination
	h2, _ := be.Launch(ctx, testSpec(2), nil) // failed

	exits := make(chan backend.HandleExit, 8)
	go func() { _ = be.Watch(ctx, func(e backend.HandleExit) { exits <- e }) }()

	cloud.setState(h1.ID, awsmicrovm.MicrovmStateTerminated)
	cloud.setState(h2.ID, awsmicrovm.MicrovmStateFailed)

	got := map[string]int{}
	for len(got) < 2 {
		select {
		case e := <-exits:
			if _, dup := got[e.ID]; dup {
				t.Fatalf("id %s reported twice", e.ID)
			}
			got[e.ID] = e.ExitCode
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out, got %v", got)
		}
	}
	if got[h1.ID] != 0 {
		t.Fatalf("clean termination should map to 0, got %d", got[h1.ID])
	}
	if got[h2.ID] != -1 {
		t.Fatalf("failed VM should map to -1, got %d", got[h2.ID])
	}

	select {
	case e := <-exits:
		t.Fatalf("unexpected duplicate exit for %s", e.ID)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestPaginationFindAndWatch(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	cloud.pageSize = 1 // force ListMicrovms to paginate one VM per page
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	be.poll = time.Millisecond

	_, err := be.Launch(ctx, testSpec(1), nil) // mvm-1, first page
	if err != nil {
		t.Fatalf("Launch 1: %v", err)
	}
	h2, err := be.Launch(ctx, testSpec(2), nil) // mvm-2, a later page
	if err != nil {
		t.Fatalf("Launch 2: %v", err)
	}

	// find must page past the first result to resolve a VM on a later page —
	// otherwise Probe would wrongly report it exited.
	if state, err := be.Probe(ctx, h2); err != nil || state != backend.StateRunning {
		t.Fatalf("Probe(h2) across pages = %v, %v; want running", state, err)
	}

	// Watch must page through too: a terminal VM on a later page still emits.
	exits := make(chan backend.HandleExit, 4)
	go func() { _ = be.Watch(ctx, func(e backend.HandleExit) { exits <- e }) }()
	cloud.setState(h2.ID, awsmicrovm.MicrovmStateTerminated)
	select {
	case e := <-exits:
		if e.ID != h2.ID || e.ExitCode != 0 {
			t.Fatalf("paged exit = %+v; want %s code 0", e, h2.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Watch dropped the exit of a VM on a later page")
	}
}

func TestWatchVanishedMicrovmReportsLost(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	be.poll = time.Millisecond

	h, _ := be.Launch(ctx, testSpec(3), nil)
	exits := make(chan backend.HandleExit, 1)
	go func() { _ = be.Watch(ctx, func(e backend.HandleExit) { exits <- e }) }()

	// Wait until Watch has observed it alive, then drop it.
	time.Sleep(20 * time.Millisecond)
	cloud.drop(h.ID)

	select {
	case e := <-exits:
		if e.ID != h.ID || e.ExitCode != -1 {
			t.Fatalf("vanished VM = %+v; want %s exit -1", e, h.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for vanished exit")
	}
}

func TestWatchIgnoresOtherRunners(t *testing.T) {
	be, cloud, _ := newTestBackend(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	be.poll = time.Millisecond

	// A MicroVM owned by a different runner.
	cloud.mu.Lock()
	cloud.vms["other-1"] = &awsmicrovm.Microvm{MicrovmID: "other-1", State: awsmicrovm.MicrovmStateRunning, Tags: map[string]string{tagRunner: "runner-2"}}
	cloud.mu.Unlock()

	exits := make(chan backend.HandleExit, 4)
	go func() { _ = be.Watch(ctx, func(e backend.HandleExit) { exits <- e }) }()

	time.Sleep(20 * time.Millisecond)
	cloud.setState("other-1", awsmicrovm.MicrovmStateTerminated)

	select {
	case e := <-exits:
		t.Fatalf("should not report another runner's MicroVM, got %+v", e)
	case <-time.After(100 * time.Millisecond):
	}
}

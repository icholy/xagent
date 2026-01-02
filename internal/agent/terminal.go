package agent

import (
	"bytes"
	"os/exec"
	"sync"
	"syscall"

	"github.com/coder/acp-go-sdk"
)

const maxOutputBytes = 1024 * 1024 // 1MB max output

// Terminal wraps an exec.Cmd with output capture and lifecycle management.
type Terminal struct {
	cmd    *exec.Cmd
	output *bytes.Buffer
	mu     sync.Mutex
	done   chan struct{}
	err    error
}

// NewTerminal creates and starts a new terminal process.
func NewTerminal(command string, args []string, cwd string, env []string) (*Terminal, error) {
	cmd := exec.Command(command, args...)
	if cwd != "" {
		cmd.Dir = cwd
	}
	if len(env) > 0 {
		cmd.Env = append(cmd.Environ(), env...)
	}

	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	t := &Terminal{
		cmd:    cmd,
		output: output,
		done:   make(chan struct{}),
	}

	// Wait for process in background
	go func() {
		t.err = cmd.Wait()
		close(t.done)
	}()

	return t, nil
}

// Output returns the captured output, truncating if necessary.
func (t *Terminal) Output() (output string, truncated bool, exitStatus *acp.TerminalExitStatus) {
	t.mu.Lock()
	defer t.mu.Unlock()

	data := t.output.Bytes()
	if len(data) > maxOutputBytes {
		data = data[len(data)-maxOutputBytes:]
		truncated = true
	}

	// Check if process has exited
	select {
	case <-t.done:
		exitStatus = t.exitStatus()
	default:
	}

	return string(data), truncated, exitStatus
}

// Wait blocks until the process exits and returns the exit status.
func (t *Terminal) Wait() (exitCode *int, signal *string) {
	<-t.done
	status := t.exitStatus()
	if status != nil {
		return status.ExitCode, status.Signal
	}
	return nil, nil
}

// Kill sends SIGKILL to the process.
func (t *Terminal) Kill() error {
	return t.cmd.Process.Kill()
}

// Done returns true if the process has exited.
func (t *Terminal) Done() bool {
	select {
	case <-t.done:
		return true
	default:
		return false
	}
}

func (t *Terminal) exitStatus() *acp.TerminalExitStatus {
	if t.cmd.ProcessState == nil {
		return nil
	}

	status := &acp.TerminalExitStatus{}

	if t.cmd.ProcessState.Exited() {
		code := t.cmd.ProcessState.ExitCode()
		status.ExitCode = &code
	} else if ws, ok := t.cmd.ProcessState.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		sig := ws.Signal().String()
		status.Signal = &sig
	}

	return status
}

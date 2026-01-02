package agent

import (
	"testing"
	"time"
)

func TestTerminal_Echo(t *testing.T) {
	term, err := NewTerminal("echo", []string{"hello", "world"}, "", nil)
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	exitCode, signal := term.Wait()
	if signal != nil {
		t.Errorf("unexpected signal: %v", *signal)
	}
	if exitCode == nil || *exitCode != 0 {
		t.Errorf("expected exit code 0, got %v", exitCode)
	}

	output, truncated, exitStatus := term.Output()
	if truncated {
		t.Error("output should not be truncated")
	}
	if output != "hello world\n" {
		t.Errorf("expected 'hello world\\n', got %q", output)
	}
	if exitStatus == nil {
		t.Error("expected exit status after wait")
	}
}

func TestTerminal_ExitCode(t *testing.T) {
	term, err := NewTerminal("sh", []string{"-c", "exit 42"}, "", nil)
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	exitCode, signal := term.Wait()
	if signal != nil {
		t.Errorf("unexpected signal: %v", *signal)
	}
	if exitCode == nil || *exitCode != 42 {
		t.Errorf("expected exit code 42, got %v", exitCode)
	}
}

func TestTerminal_OutputBeforeExit(t *testing.T) {
	term, err := NewTerminal("sh", []string{"-c", "echo start; sleep 0.1; echo end"}, "", nil)
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	// Give it time to print "start"
	time.Sleep(50 * time.Millisecond)

	output, _, exitStatus := term.Output()
	if exitStatus != nil {
		t.Error("process should not have exited yet")
	}
	if output != "start\n" {
		t.Errorf("expected 'start\\n', got %q", output)
	}

	// Wait for completion
	term.Wait()

	output, _, exitStatus = term.Output()
	if exitStatus == nil {
		t.Error("expected exit status after wait")
	}
	if output != "start\nend\n" {
		t.Errorf("expected 'start\\nend\\n', got %q", output)
	}
}

func TestTerminal_Kill(t *testing.T) {
	term, err := NewTerminal("sleep", []string{"10"}, "", nil)
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	if term.Done() {
		t.Error("process should not be done yet")
	}

	if err := term.Kill(); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	exitCode, signal := term.Wait()
	if exitCode != nil {
		t.Errorf("expected no exit code for killed process, got %v", *exitCode)
	}
	if signal == nil {
		t.Error("expected signal for killed process")
	}

	if !term.Done() {
		t.Error("process should be done after kill")
	}
}

func TestTerminal_Cwd(t *testing.T) {
	term, err := NewTerminal("pwd", nil, "/tmp", nil)
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	term.Wait()
	output, _, _ := term.Output()
	if output != "/tmp\n" {
		t.Errorf("expected '/tmp\\n', got %q", output)
	}
}

func TestTerminal_Env(t *testing.T) {
	term, err := NewTerminal("sh", []string{"-c", "echo $TEST_VAR"}, "", []string{"TEST_VAR=hello"})
	if err != nil {
		t.Fatalf("NewTerminal failed: %v", err)
	}

	term.Wait()
	output, _, _ := term.Output()
	if output != "hello\n" {
		t.Errorf("expected 'hello\\n', got %q", output)
	}
}

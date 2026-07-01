package agent

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/icholy/xagent/internal/server/shellrelay"
	"github.com/icholy/xagent/internal/shellwire"
)

// exitReportTimeout bounds the best-effort send of the final exit frame once the
// shell process has exited.
const exitReportTimeout = 5 * time.Second

// runShell serves an interactive debug shell for a reverse-shell task run. It
// allocates a PTY, spawns a login shell, dials the server's shell relay
// WebSocket for the given rendezvous session, and pipes the PTY over it using
// the shellwire framing. The relay bridges this driver leg to the operator's
// attach leg and never parses the frames.
//
// The session id is carried by the task's shell_session field; this driver
// never sets or clears it. When the shell exits, runShell sends an exit frame
// with the shell's exit code and closes the WebSocket cleanly.
func (d *Driver) runShell(ctx context.Context, session string) error {
	d.Log.Info("starting reverse shell", "session", session)

	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	cmd := exec.Command(shell)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	url, err := shellWebSocketURL(d.ServerURL, session)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{shellrelay.Subprotocol},
		HTTPHeader:   http.Header{"Authorization": {"Bearer " + d.Token}},
	})
	if err != nil {
		return fmt.Errorf("failed to dial shell relay: %w", err)
	}
	defer conn.CloseNow()

	// WebSocket -> PTY: apply incoming data/resize frames. On any read error —
	// the operator leg dropped, the session was torn down, or the parent context
	// was canceled (SIGTERM) — close the PTY so the shell gets EOF, exits, and
	// cmd.Wait returns.
	go func() {
		for {
			typ, msg, err := conn.Read(ctx)
			if err != nil {
				_ = ptmx.Close()
				return
			}
			if typ != websocket.MessageBinary {
				continue
			}
			frame, err := shellwire.Parse(msg)
			if err != nil {
				continue
			}
			switch frame.Type {
			case shellwire.TypeData:
				if _, err := ptmx.Write(frame.Payload); err != nil {
					return
				}
			case shellwire.TypeResize:
				rows, cols, err := frame.ResizeDims()
				if err != nil {
					continue
				}
				_ = pty.Setsize(ptmx, &pty.Winsize{Rows: rows, Cols: cols})
			case shellwire.TypePing:
				// keepalive; nothing to apply.
			}
		}
	}()

	// PTY -> WebSocket: stream shell output as data frames. Finishes when the
	// shell closes the PTY (read error) or the connection drops (write error).
	ptyDone := make(chan struct{})
	go func() {
		defer close(ptyDone)
		buf := make([]byte, 32*1024)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, shellwire.Data(buf[:n])); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()

	waitErr := cmd.Wait()
	// Wait for the output pump to stop before sending the exit frame: the relay
	// only tolerates one writer at a time.
	<-ptyDone

	// Send the exit frame on a context detached from ctx so it still goes out
	// during a SIGTERM-driven shutdown.
	exitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), exitReportTimeout)
	defer cancel()
	if err := conn.Write(exitCtx, websocket.MessageBinary, shellwire.Exit(exitCode(waitErr))); err != nil {
		d.Log.Debug("failed to send shell exit frame", "session", session, "error", err)
	}
	conn.Close(websocket.StatusNormalClosure, "shell exited")
	d.Log.Info("reverse shell ended", "session", session, "exit_code", exitCode(waitErr))
	return nil
}

// exitCode extracts a shell exit code from the error returned by cmd.Wait: 0 on
// success, the process exit code on a normal non-zero exit, and 1 for any other
// failure (e.g. the shell was killed).
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode()
	}
	return 1
}

// shellWebSocketURL builds the ws(s) URL for the driver leg of a session from
// the server's base URL.
func shellWebSocketURL(serverURL, session string) (string, error) {
	if serverURL == "" {
		return "", fmt.Errorf("shell: empty server URL")
	}
	switch {
	case strings.HasPrefix(serverURL, "https://"):
		serverURL = "wss://" + strings.TrimPrefix(serverURL, "https://")
	case strings.HasPrefix(serverURL, "http://"):
		serverURL = "ws://" + strings.TrimPrefix(serverURL, "http://")
	}
	return strings.TrimSuffix(serverURL, "/") + "/shell/" + session + "/driver", nil
}

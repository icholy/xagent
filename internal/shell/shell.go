// Package shell holds the client-side core of the reverse debug shell (the
// design in proposals/draft/driver-reverse-shell.md): the driver leg (Serve) and
// the operator leg (Attach), plus the shared route/URL helpers both legs and the
// server route registration use. The server-side rendezvous relay lives in the
// shellrelay sub-package, and the wire framing lives in internal/shellwire.
//
// Serve allocates a PTY, spawns a login shell, dials the server's shell relay
// WebSocket for a rendezvous session, and pipes the PTY over it using the
// shellwire framing. Attach is its operator-side counterpart: it dials the
// attach leg, drives the local terminal, and returns the shell's exit code. Both
// are plain library functions that take the server URL, the caller's token, and
// the session id, and do not depend on the driver, agent, or server packages.
//
// The wire contract lives entirely in internal/shellwire; the relay passes frames
// through opaquely and never parses them.
package shell

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/coder/websocket"
	"github.com/creack/pty"
	"github.com/icholy/xagent/internal/shellwire"
)

// exitReportTimeout bounds the best-effort send of the final exit frame once the
// shell process has exited.
const exitReportTimeout = 5 * time.Second

// CreateSessionID returns an unguessable rendezvous id for a debug-shell
// session. It is not a secret — the attach leg is authorized by org membership,
// not by knowing the id — but a random id keeps sessions from colliding and from
// being enumerable.
func CreateSessionID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generating shell session id: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// Serve runs an interactive debug shell for a rendezvous session. It allocates a
// PTY, spawns a login shell ($SHELL, else /bin/sh), dials the server's shell
// relay WebSocket at GET {serverURL}/shell/{session}/driver authenticating with
// token as a Bearer header, negotiates the xagent-shell.v1 subprotocol, and
// pipes the PTY over the WebSocket using the shellwire framing.
//
// Incoming data frames are written to the PTY master, resize frames are applied
// to the PTY, and PTY output is streamed back as data frames. When the shell
// exits, Serve sends an exit frame with the shell's exit code and closes the
// WebSocket cleanly. A dropped operator leg or a canceled ctx closes the PTY so
// the shell gets EOF, exits, and Serve returns rather than leaking a shell.
func Serve(ctx context.Context, serverURL, token, session string, log *slog.Logger) error {
	if log == nil {
		log = slog.Default()
	}
	log.Info("starting reverse shell", "session", session)

	shell := cmp.Or(os.Getenv("SHELL"), "/bin/sh")
	cmd := exec.Command(shell)
	// A leading "-" in argv[0] is the conventional signal for a login shell,
	// so it sources the profile files an operator would expect.
	cmd.Args[0] = "-" + filepath.Base(shell)
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start pty: %w", err)
	}
	defer func() { _ = ptmx.Close() }()

	url, err := DriverURL(serverURL, session)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{shellwire.Subprotocol},
		HTTPHeader:   http.Header{"Authorization": {"Bearer " + token}},
	})
	if err != nil {
		return fmt.Errorf("failed to dial shell relay: %w", err)
	}
	defer conn.CloseNow()

	// WebSocket -> PTY: apply incoming data/resize frames. On any read error —
	// the operator leg dropped, the session was torn down, or ctx was canceled —
	// close the PTY so the shell gets EOF, exits, and cmd.Wait returns.
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
	// Wait for the output pump to stop before sending the exit frame: only one
	// writer may touch the connection at a time.
	<-ptyDone

	// Send the exit frame on a context detached from ctx so it still goes out
	// during a cancellation-driven shutdown.
	exitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), exitReportTimeout)
	defer cancel()
	if err := conn.Write(exitCtx, websocket.MessageBinary, shellwire.Exit(exitCode(waitErr))); err != nil {
		log.Debug("failed to send shell exit frame", "session", session, "error", err)
	}
	conn.Close(websocket.StatusNormalClosure, "shell exited")
	log.Info("reverse shell ended", "session", session, "exit_code", exitCode(waitErr))
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

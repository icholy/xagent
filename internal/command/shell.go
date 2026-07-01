package command

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"

	"github.com/coder/websocket"
	"github.com/icholy/xagent/internal/configfile"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/shellwire"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

// ShellCommand opens an interactive debug shell in a task's sandbox. It is a
// client of the driver reverse-shell feature (step 5 of the design in
// proposals/draft/driver-reverse-shell.md): it asks the server to open a shell
// session via the OpenShell RPC, then attaches to the rendezvous relay over a
// WebSocket and pipes the local terminal through it using the shellwire framing.
// This works for any backend and for remote runners — there is no docker-direct
// path anymore.
var ShellCommand = &cli.Command{
	Name:      "shell",
	Usage:     "Open an interactive shell in a task's sandbox",
	ArgsUsage: "<task-id>",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		if cmd.NArg() < 1 {
			return cli.Exit("task ID required", 1)
		}
		taskID, err := strconv.ParseInt(cmd.Args().First(), 10, 64)
		if err != nil {
			return cli.Exit("invalid task ID: "+cmd.Args().First(), 1)
		}

		serverURL := cmd.String("server")
		cfg, err := configfile.Load(nil)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first")
		}

		// Ask the server to open a shell session for the task. The org is derived
		// from the token claims server-side; the operator leg is Bearer-only.
		client := xagentclient.New(xagentclient.Options{BaseURL: serverURL, Token: cfg.Token})
		resp, err := client.OpenShell(ctx, &xagentv1.OpenShellRequest{TaskId: taskID})
		if err != nil {
			return fmt.Errorf("failed to open shell: %w", err)
		}
		session := resp.GetSessionId()
		if session == "" {
			return fmt.Errorf("server returned an empty shell session id")
		}

		url, err := attachURL(serverURL, session)
		if err != nil {
			return err
		}
		conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
			Subprotocols: []string{shellwire.Subprotocol},
			HTTPHeader:   http.Header{"Authorization": {"Bearer " + cfg.Token}},
		})
		if err != nil {
			return fmt.Errorf("failed to attach to shell: %w", err)
		}
		defer conn.CloseNow()

		// Put the terminal into raw mode so keystrokes reach the remote shell
		// unbuffered and unechoed; always restore it on the way out.
		fd := int(os.Stdin.Fd())
		if term.IsTerminal(fd) {
			state, err := term.MakeRaw(fd)
			if err != nil {
				return fmt.Errorf("failed to set raw terminal mode: %w", err)
			}
			defer func() { _ = term.Restore(fd, state) }()
		}

		// Track terminal size: seed the initial size, then follow SIGWINCH.
		resize := make(chan winSize, 1)
		if cols, rows, err := term.GetSize(fd); err == nil {
			resize <- winSize{rows: uint16(rows), cols: uint16(cols)}
		}
		sigwinch := make(chan os.Signal, 1)
		signal.Notify(sigwinch, syscall.SIGWINCH)
		defer signal.Stop(sigwinch)
		go func() {
			for range sigwinch {
				cols, rows, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				select {
				case resize <- winSize{rows: uint16(rows), cols: uint16(cols)}:
				case <-ctx.Done():
					return
				}
			}
		}()

		code, err := pump(ctx, conn, os.Stdin, os.Stdout, resize)
		if err != nil {
			return fmt.Errorf("shell session ended: %w", err)
		}
		conn.Close(websocket.StatusNormalClosure, "shell exited")
		return cli.Exit("", code)
	},
}

// winSize is a terminal size in rows and cols.
type winSize struct {
	rows, cols uint16
}

// wsConn is the subset of *websocket.Conn the frame pump uses. Abstracting it
// keeps pump testable against an in-memory fake without a real network dial.
type wsConn interface {
	Read(ctx context.Context) (websocket.MessageType, []byte, error)
	Write(ctx context.Context, typ websocket.MessageType, p []byte) error
}

// pump runs the operator side of the shellwire protocol over conn until an Exit
// frame arrives — returning its code — or the connection errors. It reads local
// input from in and sends it as Data frames, writes incoming Data frames to out,
// and forwards terminal sizes from resize as Resize frames. Ping frames are
// keepalives and ignored.
func pump(ctx context.Context, conn wsConn, in io.Reader, out io.Writer, resize <-chan winSize) (int, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Serialize outgoing frames through a single writer goroutine: coder/websocket
	// permits only one concurrent writer, and both input and resize produce frames.
	frames := make(chan []byte)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := in.Read(buf)
			if n > 0 {
				select {
				case frames <- shellwire.Data(buf[:n]):
				case <-ctx.Done():
					return
				}
			}
			if err != nil {
				return
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case ws, ok := <-resize:
				if !ok {
					return
				}
				select {
				case frames <- shellwire.Resize(ws.rows, ws.cols):
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case frame := <-frames:
				if err := conn.Write(ctx, websocket.MessageBinary, frame); err != nil {
					cancel()
					return
				}
			}
		}
	}()

	for {
		typ, msg, err := conn.Read(ctx)
		if err != nil {
			return 1, err
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
			if _, err := out.Write(frame.Payload); err != nil {
				return 1, err
			}
		case shellwire.TypeExit:
			code, err := frame.ExitCode()
			if err != nil {
				return 1, err
			}
			return code, nil
		case shellwire.TypePing:
			// keepalive; nothing to do.
		}
	}
}

// attachURL builds the ws(s) URL for the operator attach leg of a session from
// the server's base URL, mirroring the scheme handling in internal/shell.
func attachURL(serverURL, session string) (string, error) {
	if serverURL == "" {
		return "", fmt.Errorf("shell: empty server URL")
	}
	switch {
	case strings.HasPrefix(serverURL, "https://"):
		serverURL = "wss://" + strings.TrimPrefix(serverURL, "https://")
	case strings.HasPrefix(serverURL, "http://"):
		serverURL = "ws://" + strings.TrimPrefix(serverURL, "http://")
	}
	return strings.TrimSuffix(serverURL, "/") + "/shell/" + session + "/attach", nil
}

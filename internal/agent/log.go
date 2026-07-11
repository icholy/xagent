package agent

import (
	"io"
	"os"
	"path"
)

// DefaultLogPath is the in-sandbox location of the driver's append-only log
// file. Like DefaultConfigStore, it is a fixed convention shared across the
// runner/driver boundary: the runner pre-creates its parent directory (0777)
// and the driver tees all of its output into it so a completed run can be
// inspected post-mortem via the reverse-shell. It lives under /xagent (the
// container's writable layer, preserved across adopted runs) rather than
// /tmp, which may be a tmpfs or cleared by a setup step.
const DefaultLogPath = "/xagent/log"

// nopWriteCloser adds a no-op Close to an io.Writer so the caller can always
// defer Close regardless of whether the real file opened.
type nopWriteCloser struct {
	io.Writer
}

func (nopWriteCloser) Close() error { return nil }

// OpenLogSink opens the append-only log file at logPath, creating its parent
// directory as a fallback (the runner normally pre-creates it so a non-root
// driver can write there). The file is opened O_CREATE|O_WRONLY|O_APPEND, so
// an existing file is appended to, never truncated.
//
// Opening is best-effort: on any filesystem failure it returns an
// io.Discard-backed no-op WriteCloser alongside the error, so the caller can
// log the failure but the sink is always usable and a run never fails because
// logging could not be set up. The returned WriteCloser must be closed.
func OpenLogSink(logPath string) (io.WriteCloser, error) {
	// The runner pre-creates the dir 0777; this MkdirAll is only a fallback for
	// a directly-invoked driver outside the runner. Its error is not fatal —
	// OpenFile below is the real check and degrades gracefully.
	_ = os.MkdirAll(path.Dir(logPath), 0o777)
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o666)
	if err != nil {
		return nopWriteCloser{io.Discard}, err
	}
	return f, nil
}

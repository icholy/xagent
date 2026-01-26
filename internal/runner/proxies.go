package runner

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/icholy/xagent/internal/xagentclient"
)

// TaskProxies manages per-task Unix socket proxies.
type TaskProxies struct {
	serverURL string
	auth      xagentclient.TokenSource
	log       *slog.Logger

	mu      sync.Mutex
	proxies map[int64]*xagentclient.UnixProxy
}

// NewTaskProxies creates a new TaskProxies manager.
func NewTaskProxies(serverURL string, auth xagentclient.TokenSource, log *slog.Logger) *TaskProxies {
	return &TaskProxies{
		serverURL: serverURL,
		auth:      auth,
		log:       log,
		proxies:   make(map[int64]*xagentclient.UnixProxy),
	}
}

// Start creates and starts a proxy for a task, returning the socket path.
// If a proxy already exists for the task, it returns the existing socket path.
func (p *TaskProxies) Start(taskID int64) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if proxy already exists
	if proxy, ok := p.proxies[taskID]; ok {
		return proxy.SocketPath(), nil
	}

	path := fmt.Sprintf("/tmp/xagent-%d.sock", taskID)
	proxy, err := xagentclient.NewUnixProxy(path, p.serverURL, p.auth)
	if err != nil {
		return "", err
	}

	go func() {
		if err := proxy.Serve(); err != http.ErrServerClosed {
			p.log.Error("proxy failed", "task", taskID, "err", err)
		}
	}()

	p.proxies[taskID] = proxy
	p.log.Debug("started proxy", "task", taskID, "socket", path)
	return path, nil
}

// Stop closes and removes the proxy for a task.
func (p *TaskProxies) Stop(taskID int64) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	proxy, ok := p.proxies[taskID]
	if !ok {
		return nil
	}

	err := proxy.Close()
	delete(p.proxies, taskID)
	p.log.Debug("stopped proxy", "task", taskID)
	return err
}

// Close stops all proxies.
func (p *TaskProxies) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var lastErr error
	for taskID, proxy := range p.proxies {
		if err := proxy.Close(); err != nil {
			p.log.Error("failed to close proxy", "task", taskID, "error", err)
			lastErr = err
		}
	}
	p.proxies = make(map[int64]*xagentclient.UnixProxy)
	return lastErr
}

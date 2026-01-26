package runner

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/icholy/xagent/internal/model"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/icholy/xagent/internal/xmcp"
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

// Get an existing proxy
func (p *TaskProxies) Get(taskID int64) (*xagentclient.UnixProxy, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	proxy, ok := p.proxies[taskID]
	return proxy, ok
}

// Start creates and starts a proxy for a task, returning the socket path.
// If a proxy already exists for the task, it returns the existing socket path.
func (p *TaskProxies) Start(task *model.Task) (*xagentclient.UnixProxy, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if proxy already exists
	if proxy, ok := p.proxies[task.ID]; ok {
		return proxy, nil
	}

	// Create client to the upstream server
	client := xagentclient.New(p.serverURL, p.auth)

	// Create filter to enforce access control
	filter := xmcp.NewAgentFilter(task, client)

	// Create Connect RPC handler
	mux := http.NewServeMux()
	mux.Handle(xagentv1connect.NewXAgentServiceHandler(filter))

	path := fmt.Sprintf("/tmp/xagent-%d.sock", task.ID)
	proxy, err := xagentclient.NewUnixProxy(path, mux)
	if err != nil {
		return nil, err
	}

	go func() {
		if err := proxy.Serve(); err != http.ErrServerClosed {
			p.log.Error("proxy failed", "task", task.ID, "err", err)
		}
	}()

	p.proxies[task.ID] = proxy
	p.log.Debug("started proxy", "task", task.ID, "socket", path)
	return proxy, nil
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

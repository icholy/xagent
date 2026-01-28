package runner

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/icholy/xagent/internal/agentauth"
	"github.com/icholy/xagent/internal/proto/xagent/v1/xagentv1connect"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/icholy/xagent/internal/xmcp"
)

// AgentProxy manages a single Unix socket proxy for all tasks.
type AgentProxy struct {
	serverURL  string
	auth       xagentclient.TokenSource
	privateKey ed25519.PrivateKey
	log        *slog.Logger
	proxy      *xagentclient.UnixProxy
	socketPath string
}

// NewProxy creates a new Proxy. The secretFile is the path to the Ed25519
// private key used for signing and verifying agent tokens. If the file does
// not exist, a new key is generated and saved.
func NewProxy(serverURL string, auth xagentclient.TokenSource, secretFile string, log *slog.Logger) (*AgentProxy, error) {
	privateKey, err := agentauth.LoadOrCreatePrivateKey(secretFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load private key: %w", err)
	}
	return &AgentProxy{
		serverURL:  serverURL,
		auth:       auth,
		privateKey: privateKey,
		log:        log,
		socketPath: randomSocketPath(),
	}, nil
}

// randomSocketPath generates a random socket path in the system temp directory.
func randomSocketPath() string {
	var b [8]byte
	rand.Read(b[:])
	return filepath.Join(os.TempDir(), "xagent-"+hex.EncodeToString(b[:])+".sock")
}

// SocketPath returns the path to the Unix socket.
func (p *AgentProxy) SocketPath() string {
	return p.socketPath
}

// SignToken creates a JWT signed with the proxy's private key.
func (p *AgentProxy) SignToken(claims *agentauth.TaskClaims) (string, error) {
	return agentauth.SignToken(p.privateKey, claims)
}

// Start creates and starts the proxy.
func (p *AgentProxy) Start() error {
	if p.proxy != nil {
		return fmt.Errorf("proxy already started")
	}

	// Create client to the upstream server
	client := xagentclient.New(p.serverURL, p.auth)

	// Create filter to enforce access control
	filter := xmcp.NewAgentFilter(client)

	// Create Connect RPC handler
	path, handler := xagentv1connect.NewXAgentServiceHandler(filter)

	// Wrap with token middleware
	mux := http.NewServeMux()
	mux.Handle(path, agentauth.Middleware(p.privateKey)(handler))

	proxy, err := xagentclient.NewUnixProxy(p.SocketPath(), mux)
	if err != nil {
		return err
	}

	go func() {
		if err := proxy.Serve(); err != http.ErrServerClosed {
			p.log.Error("proxy failed", "err", err)
		}
	}()

	p.proxy = proxy
	p.log.Debug("started proxy", "socket", p.SocketPath())
	return nil
}

// Close stops the proxy.
func (p *AgentProxy) Close() error {
	if p.proxy == nil {
		return nil
	}
	err := p.proxy.Close()
	p.proxy = nil
	p.log.Debug("stopped proxy")
	return err
}

package xagentclient

import (
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

type UnixProxy struct {
	socketPath string
	listener   net.Listener
	server     *http.Server
}

func NewUnixProxy(socketPath, targetURL string, tokenSource TokenSource) (*UnixProxy, error) {
	target, err := url.Parse(targetURL)
	if err != nil {
		return nil, err
	}

	// Remove existing socket file
	os.Remove(socketPath)

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, err
	}

	// Make socket world-accessible
	os.Chmod(socketPath, 0777)

	proxy := httputil.NewSingleHostReverseProxy(target)

	// Wrap transport to inject auth header
	if tokenSource != nil {
		proxy.Transport = &AuthTransport{
			Transport: http.DefaultTransport,
			Source:    tokenSource,
		}
	}

	return &UnixProxy{
		socketPath: socketPath,
		listener:   listener,
		server: &http.Server{
			Handler: proxy,
		},
	}, nil
}

func (p *UnixProxy) SocketPath() string {
	return p.socketPath
}

func (p *UnixProxy) Serve() error {
	return p.server.Serve(p.listener)
}

func (p *UnixProxy) Close() error {
	p.server.Close()
	os.Remove(p.socketPath)
	return nil
}

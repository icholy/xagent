package xagentclient

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
)

type UnixProxy struct {
	path     string
	listener net.Listener
	server   *http.Server
}

func NewUnixProxy(path string, handler http.Handler) (*UnixProxy, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, err
	}

	// Remove existing socket file or directory (Docker creates a directory
	// if the socket path doesn't exist when bind mounting).
	if err := os.RemoveAll(path); err != nil {
		return nil, err
	}

	listener, err := net.Listen("unix", path)
	if err != nil {
		return nil, err
	}

	// Make socket world-accessible
	os.Chmod(path, 0777)

	return &UnixProxy{
		path:     path,
		listener: listener,
		server: &http.Server{
			Handler: handler,
		},
	}, nil
}

func (p *UnixProxy) SocketPath() string {
	return p.path
}

func (p *UnixProxy) Serve() error {
	return p.server.Serve(p.listener)
}

func (p *UnixProxy) Close() error {
	p.server.Close()
	os.Remove(p.path)
	return nil
}

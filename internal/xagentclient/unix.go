package xagentclient

import (
	"errors"
	"net"
	"net/http"
	"os"
)

type UnixProxy struct {
	path     string
	listener net.Listener
	server   *http.Server
}

func NewUnixProxy(path string, handler http.Handler) (*UnixProxy, error) {
	// Remove existing socket file
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
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

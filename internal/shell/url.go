package shell

import (
	"fmt"
	"strings"
)

// The two legs of a rendezvous session are exposed under a shared path prefix.
// The path segments are defined once here and reused by the client URL builders
// below and by the server's route registration (shell.DriverRoute /
// shell.AttachRoute), so the wire path has a single definition.
const (
	routePrefix     = "/shell/"
	sessionWildcard = "{session}"
	driverLeg       = "/driver"
	attachLeg       = "/attach"
)

// DriverRoute and AttachRoute are the ServeMux patterns for the two legs. The
// {session} wildcard is read back by the relay handlers with
// req.PathValue("session").
const (
	DriverRoute = "GET " + routePrefix + sessionWildcard + driverLeg
	AttachRoute = "GET " + routePrefix + sessionWildcard + attachLeg
)

// DriverURL builds the ws(s) URL for the driver leg of a session from the
// server's base URL.
func DriverURL(serverURL, session string) (string, error) {
	return legURL(serverURL, session, driverLeg)
}

// AttachURL builds the ws(s) URL for the operator attach leg of a session from
// the server's base URL.
func AttachURL(serverURL, session string) (string, error) {
	return legURL(serverURL, session, attachLeg)
}

// legURL converts an http(s) base URL to its ws(s) equivalent and appends the
// leg's path for the session.
func legURL(serverURL, session, leg string) (string, error) {
	if serverURL == "" {
		return "", fmt.Errorf("shell: empty server URL")
	}
	switch {
	case strings.HasPrefix(serverURL, "https://"):
		serverURL = "wss://" + strings.TrimPrefix(serverURL, "https://")
	case strings.HasPrefix(serverURL, "http://"):
		serverURL = "ws://" + strings.TrimPrefix(serverURL, "http://")
	}
	return strings.TrimSuffix(serverURL, "/") + routePrefix + session + leg, nil
}

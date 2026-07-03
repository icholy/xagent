package shell

import (
	"fmt"
	"strings"
)

// The two legs of a rendezvous session are exposed under a shared path prefix.
// The segments are defined once here so the client URL builders below share a
// single definition of the wire path; the server registers the matching routes
// as literals.
const (
	routePrefix = "/shell/"
	driverLeg   = "/driver"
	attachLeg   = "/attach"
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

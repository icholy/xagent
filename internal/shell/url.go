package shell

import (
	"fmt"
	"net/url"
	"strings"
)

// The two legs of a rendezvous session are exposed under a shared path prefix
// with the session id carried as a ?session= query parameter. The patterns are
// defined once here so the client URL builders and the server route
// registration (via DriverRoute/AttachRoute) share a single definition of the
// wire path.
const (
	routePrefix = "/shell/"
	driverLeg   = "driver"
	attachLeg   = "attach"

	// DriverRoute and AttachRoute are the net/http mux patterns for the two legs.
	DriverRoute = "GET " + routePrefix + driverLeg
	AttachRoute = "GET " + routePrefix + attachLeg
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
// leg's path with the session id as a ?session= query parameter (escaped via
// net/url).
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
	query := url.Values{"session": {session}}.Encode()
	return strings.TrimSuffix(serverURL, "/") + routePrefix + leg + "?" + query, nil
}

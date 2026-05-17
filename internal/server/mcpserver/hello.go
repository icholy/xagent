package mcpserver

import (
	"net/http"
	"strings"
)

// HelloMiddleware intercepts browser navigations to the MCP endpoint and
// serves a human-readable page explaining what the endpoint is for. MCP
// clients (which advertise application/json or text/event-stream) and any
// non-GET requests pass through to next unchanged.
//
// Without this, a user pasting the MCP URL into a browser address bar sees
// a raw 401 / JSON blob and assumes the server is broken.
func HelloMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isBrowserRequest(r) {
			serveHelloPage(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func isBrowserRequest(r *http.Request) bool {
	if r.Method != http.MethodGet {
		return false
	}
	accept := r.Header.Get("Accept")
	if !strings.Contains(accept, "text/html") {
		return false
	}
	if strings.Contains(accept, "application/json") || strings.Contains(accept, "text/event-stream") {
		return false
	}
	return true
}

func serveHelloPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(helloHTML))
}

const helloHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>xagent MCP server</title>
<style>
  :root { color-scheme: light dark; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
    max-width: 42rem;
    margin: 4rem auto;
    padding: 0 1.25rem;
    line-height: 1.55;
  }
  h1 { margin-bottom: 0.25rem; }
  .lede { color: #666; margin-top: 0; }
  code {
    background: rgba(127, 127, 127, 0.15);
    padding: 0.1rem 0.35rem;
    border-radius: 4px;
    font-size: 0.95em;
  }
  pre {
    background: rgba(127, 127, 127, 0.12);
    padding: 0.9rem 1rem;
    border-radius: 6px;
    overflow-x: auto;
  }
  hr { border: none; border-top: 1px solid rgba(127, 127, 127, 0.25); margin: 2rem 0; }
  footer { color: #888; font-size: 0.9em; }
</style>
</head>
<body>
  <h1>xagent MCP server</h1>
  <p class="lede">You've reached an MCP (Model Context Protocol) endpoint. It's meant to be configured in an MCP client, not browsed directly.</p>

  <h2>Add it to your client</h2>
  <p>Copy the full URL of this page and paste it into your MCP client's "add server" flow as the server URL. You'll also need an API key or OAuth credentials from your xagent account.</p>

  <h2>Example: Claude Code</h2>
  <pre><code>claude mcp add --transport http xagent &lt;this-url&gt; \
    --header "Authorization: Bearer &lt;your-api-key&gt;"</code></pre>

  <h2>Example: client config file</h2>
  <pre><code>{
  "mcpServers": {
    "xagent": {
      "type": "http",
      "url": "&lt;this-url&gt;",
      "headers": { "Authorization": "Bearer &lt;your-api-key&gt;" }
    }
  }
}</code></pre>

  <hr>
  <footer>
    Not seeing what you expected? Open the <a href="/ui/">web UI</a>, or learn more about MCP at <a href="https://modelcontextprotocol.io">modelcontextprotocol.io</a>.
  </footer>
</body>
</html>
`

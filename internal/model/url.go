package model

import (
	"fmt"
	"net/url"
	"strings"
)

// TaskURL returns the user-facing UI URL for a task, including the org query
// parameter so deep links resolve to the correct org for users with
// multiple memberships.
func TaskURL(baseURL string, taskID, orgID int64) string {
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/ui/tasks/%d?org=%d", baseURL, taskID, orgID)
}

// RoutingURL reduces a recognized resource URL to a stable routing key, so two
// URLs that point at the same logical resource — a comment vs. its issue, or an
// API URL vs. its web URL — produce the same key. Only recognized URLs (GitHub
// issues/PRs and Jira issues, in both their web and API forms) are normalized;
// anything else is returned unchanged.
//
//	github.com/o/r/issues/5#issuecomment-9            -> github.com/o/r/issues/5
//	github.com/o/r/pull/5/files                       -> github.com/o/r/pull/5
//	api.github.com/repos/o/r/issues/5                 -> github.com/o/r/issues/5
//	api.github.com/repos/o/r/pulls/5                  -> github.com/o/r/pull/5
//	site.atlassian.net/browse/X-1?focusedCommentId=2  -> site.atlassian.net/browse/X-1
//	site.atlassian.net/rest/api/2/issue/X-1           -> site.atlassian.net/browse/X-1
func RoutingURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	switch {
	case u.Host == "github.com":
		if key, ok := githubWebRoutingURL(u); ok {
			return key
		}
	case u.Host == "api.github.com":
		if key, ok := githubAPIRoutingURL(u); ok {
			return key
		}
	case strings.HasSuffix(u.Host, ".atlassian.net"):
		if key, ok := jiraRoutingURL(u); ok {
			return key
		}
	}
	return raw
}

// githubWebRoutingURL reduces a github.com web URL of the form
// /{owner}/{repo}/{issues|pull}/{n}[/...] to its canonical resource URL,
// dropping fragments and trailing path segments (/files, /commits/…).
func githubWebRoutingURL(u *url.URL) (string, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 4 {
		return "", false
	}
	owner, repo, kind, number := parts[0], parts[1], parts[2], parts[3]
	if kind != "issues" && kind != "pull" {
		return "", false
	}
	if !isAllDigits(number) {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s/%s", owner, repo, kind, number), true
}

// githubAPIRoutingURL reduces an api.github.com URL of the form
// /repos/{owner}/{repo}/{issues|pulls}/{n} to the matching web resource URL.
// API comment URLs that don't embed the parent number can't be reduced.
func githubAPIRoutingURL(u *url.URL) (string, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < 5 || parts[0] != "repos" {
		return "", false
	}
	owner, repo, kind, number := parts[1], parts[2], parts[3], parts[4]
	var webKind string
	switch kind {
	case "issues":
		webKind = "issues"
	case "pulls":
		webKind = "pull"
	default:
		return "", false
	}
	if !isAllDigits(number) {
		return "", false
	}
	return fmt.Sprintf("https://github.com/%s/%s/%s/%s", owner, repo, webKind, number), true
}

// jiraRoutingURL reduces a *.atlassian.net browse URL (dropping query params
// like focusedCommentId) or a REST API issue URL to the canonical browse URL.
// API URLs that use a numeric issue id can't be mapped to a key without a
// lookup, so they're left unchanged.
func jiraRoutingURL(u *url.URL) (string, bool) {
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "browse" {
		return fmt.Sprintf("https://%s/browse/%s", u.Host, parts[1]), true
	}
	if len(parts) >= 5 && parts[0] == "rest" && parts[1] == "api" && parts[3] == "issue" {
		key := parts[4]
		if isAllDigits(key) {
			return "", false
		}
		return fmt.Sprintf("https://%s/browse/%s", u.Host, key), true
	}
	return "", false
}

// isAllDigits reports whether s is non-empty and consists solely of ASCII digits.
func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

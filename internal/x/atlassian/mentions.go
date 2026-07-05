package atlassian

import "regexp"

// mentionRe captures the account ID from Jira's [~accountid:…] mention syntax.
var mentionRe = regexp.MustCompile(`\[~accountid:([^\]]+)\]`)

// Mentions returns the account IDs mentioned in a comment body via Jira's
// [~accountid:…] syntax, in order of appearance. It returns nil when the body
// mentions no one.
func Mentions(body string) []string {
	var ids []string
	for _, m := range mentionRe.FindAllStringSubmatch(body, -1) {
		ids = append(ids, m[1])
	}
	return ids
}

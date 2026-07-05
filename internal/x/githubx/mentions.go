package githubx

import "regexp"

// mentionRe locates a GitHub @mention: an "@" preceded by start-of-string,
// whitespace, or "(", capturing the login. The trailing word boundary is
// verified separately (see Mentions) rather than matched here so a consumed
// trailing space can't hide an adjacent mention like "@alice @bob".
var mentionRe = regexp.MustCompile(`(?i)(?:^|[\s(])@([A-Za-z0-9-]+)`)

// Mentions returns the GitHub logins @mentioned in text, in order of
// appearance. It uses the same word-boundary rule the router applies when
// testing a single login (`(?:^|[\s(])@name(?:$|[\s,.)!?])`), generalized to
// extract every login, so "@alice/team" is a team reference rather than a
// mention of "alice". It returns nil when text mentions no one.
func Mentions(text string) []string {
	var logins []string
	for _, loc := range mentionRe.FindAllStringSubmatchIndex(text, -1) {
		end := loc[3] // end index of the captured login
		if end < len(text) && !isMentionBoundary(text[end]) {
			continue
		}
		logins = append(logins, text[loc[2]:end])
	}
	return logins
}

// isMentionBoundary reports whether b is one of the characters accepted
// immediately after a login (the `(?:$|[\s,.)!?])` set, with \s expanded).
func isMentionBoundary(b byte) bool {
	switch b {
	case ' ', '\t', '\n', '\r', '\f', ',', '.', ')', '!', '?':
		return true
	default:
		return false
	}
}

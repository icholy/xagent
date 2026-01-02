package expandvar

import (
	"regexp"
)

var pattern = regexp.MustCompile(`\$\{([^:}]+):([^}]+)\}`)

// Expand replaces ${namespace:value} patterns in the input string
// using the provided replace function.
func Expand(input string, replace func(namespace, value string) (string, error)) (string, error) {
	var lastErr error
	result := pattern.ReplaceAllStringFunc(input, func(match string) string {
		parts := pattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		expanded, err := replace(parts[1], parts[2])
		if err != nil {
			lastErr = err
			return match
		}
		return expanded
	})
	return result, lastErr
}

package model

import "fmt"

// TaskURL returns the user-facing UI URL for a task, including the org query
// parameter so deep links resolve to the correct org for users with
// multiple memberships.
func TaskURL(baseURL string, taskID, orgID int64) string {
	if baseURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/ui/tasks/%d?org=%d", baseURL, taskID, orgID)
}

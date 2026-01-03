package jirax

import (
	"context"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

// StatusList returns a list of jira issue status names.
// If category is not empty, only statuses in that category will be returned.
func StatusList(ctx context.Context, client *jira.Client, category string) ([]string, error) {
	all, _, err := client.Status.GetAllStatuses(ctx)
	if err != nil {
		return nil, err
	}
	names := []string{}
	for _, s := range all {
		if category == "" || s.StatusCategory.Name == category {
			names = append(names, s.Name)
		}
	}
	return names, nil
}

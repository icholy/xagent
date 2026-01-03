package jirax

import (
	"fmt"
	"strings"
)

// JQL represents a query in JIRA Query Language.
type JQL struct {
	Projects    []string
	Labels      []string
	Status      []string
	NotStatus   []string
	ParentEpics []string
}

// String converts the JQL struct into a formatted JQL string.
func (q JQL) String() string {
	var jql strings.Builder
	if len(q.Projects) > 0 {
		fmt.Fprintf(&jql, "project IN (%s)", formatValues(q.Projects))
	}
	if len(q.Labels) > 0 {
		if jql.Len() > 0 {
			jql.WriteString(" AND ")
		}
		fmt.Fprintf(&jql, "labels IN (%s)", formatValues(q.Labels))
	}
	if len(q.Status) > 0 {
		if jql.Len() > 0 {
			jql.WriteString(" AND ")
		}
		fmt.Fprintf(&jql, "status IN (%s)", formatValues(q.Status))
	}
	if len(q.NotStatus) > 0 {
		if jql.Len() > 0 {
			jql.WriteString(" AND ")
		}
		fmt.Fprintf(&jql, "status NOT IN (%s)", formatValues(q.NotStatus))
	}
	if len(q.ParentEpics) > 0 {
		if jql.Len() > 0 {
			jql.WriteString(" AND ")
		}
		fmt.Fprintf(&jql, "parentEpic IN (%s)", formatValues(q.ParentEpics))
	}
	return jql.String()
}

func formatValues(values []string) string {
	quoted := make([]string, len(values))
	for i, value := range values {
		quoted[i] = fmt.Sprintf("%q", value)
	}
	return strings.Join(quoted, ", ")
}

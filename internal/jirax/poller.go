package jirax

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
)

type Comment struct {
	ID       string
	IssueKey string
	IssueURL string
	Author   string
	Body     string
}

type PollerOptions struct {
	Client    *jira.Client
	JQL       JQL
	Username  string
	Interval  time.Duration
	StateFile string
	OnComment func(Comment)
}

type Poller struct {
	opts PollerOptions
	seen map[string]bool
}

func NewPoller(opts PollerOptions) *Poller {
	p := &Poller{
		opts: opts,
		seen: make(map[string]bool),
	}
	p.loadState()
	return p
}

func (p *Poller) loadState() {
	if p.opts.StateFile == "" {
		return
	}
	data, err := os.ReadFile(p.opts.StateFile)
	if err != nil {
		return
	}
	var ids []string
	if err := json.Unmarshal(data, &ids); err != nil {
		return
	}
	for _, id := range ids {
		p.seen[id] = true
	}
	slog.Info("loaded state", "file", p.opts.StateFile, "count", len(ids))
}

func (p *Poller) saveState() {
	if p.opts.StateFile == "" {
		return
	}
	ids := make([]string, 0, len(p.seen))
	for id := range p.seen {
		ids = append(ids, id)
	}
	data, err := json.Marshal(ids)
	if err != nil {
		slog.Error("failed to marshal state", "error", err)
		return
	}
	if err := os.WriteFile(p.opts.StateFile, data, 0644); err != nil {
		slog.Error("failed to save state", "error", err)
	}
}

func (p *Poller) Run(ctx context.Context) error {
	for {
		comments, err := p.search(ctx)
		if err != nil {
			slog.Error("failed to fetch comments", "error", err)
		} else {
			for _, c := range comments {
				if p.seen[c.ID] {
					continue
				}
				p.seen[c.ID] = true
				if p.opts.OnComment != nil {
					p.opts.OnComment(c)
				}
			}
			p.saveState()
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.opts.Interval):
		}
	}
}

func (p *Poller) search(ctx context.Context) ([]Comment, error) {
	jql := p.opts.JQL.String()
	issues, _, err := p.opts.Client.Issue.Search(ctx, jql, nil)
	if err != nil {
		return nil, err
	}

	var comments []Comment

	for _, issue := range issues {
		issueDetail, _, err := p.opts.Client.Issue.Get(ctx, issue.Key, &jira.GetQueryOptions{
			Expand: "renderedFields",
		})
		if err != nil {
			slog.Error("failed to get issue", "key", issue.Key, "error", err)
			continue
		}

		if issueDetail.Fields.Comments == nil {
			continue
		}

		for _, c := range issueDetail.Fields.Comments.Comments {
			if !strings.EqualFold(c.Author.DisplayName, p.opts.Username) &&
				!strings.EqualFold(c.Author.EmailAddress, p.opts.Username) {
				continue
			}
			comments = append(comments, Comment{
				ID:       c.ID,
				IssueKey: issue.Key,
				IssueURL: issueDetail.Self,
				Author:   c.Author.DisplayName,
				Body:     c.Body,
			})
		}
	}

	return comments, nil
}

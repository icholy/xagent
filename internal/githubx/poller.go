package githubx

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
)

type Comment struct {
	ID       int64
	PRNumber int
	PRURL    string
	Author   string
	Body     string
	URL      string
}

type PollerOptions struct {
	Client    *github.Client
	Owner     string
	Repo      string
	Username  string
	Keyword   string
	Label     string
	Interval  time.Duration
	OnComment func(Comment)
}

type Poller struct {
	opts PollerOptions
	seen map[int64]bool
}

func NewPoller(opts PollerOptions) *Poller {
	return &Poller{
		opts: opts,
		seen: make(map[int64]bool),
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
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.opts.Interval):
		}
	}
}

func (p *Poller) search(ctx context.Context) ([]Comment, error) {
	query := "repo:" + p.opts.Owner + "/" + p.opts.Repo + " is:pr is:open label:" + p.opts.Label
	result, _, err := p.opts.Client.Search.Issues(ctx, query, nil)
	if err != nil {
		return nil, err
	}

	var comments []Comment

	for _, issue := range result.Issues {
		prNumber := issue.GetNumber()

		issueComments, _, err := p.opts.Client.Issues.ListComments(ctx, p.opts.Owner, p.opts.Repo, prNumber, nil)
		if err != nil {
			slog.Error("failed to get comments", "pr", prNumber, "error", err)
			continue
		}

		for _, c := range issueComments {
			if !strings.EqualFold(c.GetUser().GetLogin(), p.opts.Username) {
				continue
			}
			if !strings.Contains(strings.ToLower(c.GetBody()), strings.ToLower(p.opts.Keyword)) {
				continue
			}
			comments = append(comments, Comment{
				ID:       c.GetID(),
				PRNumber: prNumber,
				PRURL:    issue.GetHTMLURL(),
				Author:   c.GetUser().GetLogin(),
				Body:     c.GetBody(),
				URL:      c.GetHTMLURL(),
			})
		}
	}

	return comments, nil
}

package command

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/githubx"
	"github.com/urfave/cli/v3"
)

var GithubCommand = &cli.Command{
	Name:  "github",
	Usage: "Poll GitHub for PR comments",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "repo",
			Aliases:  []string{"r"},
			Usage:    "Repository (owner/repo)",
			Required: true,
		},
		&cli.StringFlag{
			Name:     "username",
			Aliases:  []string{"u"},
			Usage:    "GitHub username to filter by",
			Required: true,
		},
		&cli.StringFlag{
			Name:  "keyword",
			Usage: "Keyword to search for in comments",
			Value: "xagent",
		},
		&cli.StringFlag{
			Name:  "label",
			Usage: "Label to filter PRs by",
			Value: "xagent",
		},
		&cli.DurationFlag{
			Name:  "interval",
			Usage: "Poll interval",
			Value: 30 * time.Second,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		repo := cmd.String("repo")
		username := cmd.String("username")
		keyword := cmd.String("keyword")
		label := cmd.String("label")
		interval := cmd.Duration("interval")

		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			return cli.Exit("repo must be in owner/repo format", 1)
		}
		owner, repoName := parts[0], parts[1]

		client := github.NewClient(nil)
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			client = client.WithAuthToken(token)
		}

		slog.Info("starting github poller",
			"repo", repo,
			"username", username,
			"keyword", keyword,
			"label", label,
			"interval", interval,
		)

		poller := githubx.NewPoller(githubx.PollerOptions{
			Client:   client,
			Owner:    owner,
			Repo:     repoName,
			Username: username,
			Keyword:  keyword,
			Label:    label,
			Interval: interval,
			OnComment: func(c githubx.Comment) {
				slog.Info("found comment",
					"pr", c.PRNumber,
					"author", c.Author,
					"body", c.Body,
					"url", c.URL,
				)
			},
		})

		return poller.Run(ctx)
	},
}

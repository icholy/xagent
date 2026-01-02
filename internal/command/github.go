package command

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/google/go-github/v68/github"
	"github.com/icholy/xagent/internal/githubx"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
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
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "xagent server URL",
			Value:   "http://localhost:8080",
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		repo := cmd.String("repo")
		username := cmd.String("username")
		keyword := cmd.String("keyword")
		label := cmd.String("label")
		interval := cmd.Duration("interval")
		serverURL := cmd.String("server")

		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			return cli.Exit("repo must be in owner/repo format", 1)
		}
		owner, repoName := parts[0], parts[1]

		ghClient := github.NewClient(nil)
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			ghClient = ghClient.WithAuthToken(token)
		}

		xagent := xagentclient.New(serverURL)

		slog.Info("starting github poller",
			"repo", repo,
			"username", username,
			"keyword", keyword,
			"label", label,
			"interval", interval,
		)

		poller := githubx.NewPoller(githubx.PollerOptions{
			Client:   ghClient,
			Owner:    owner,
			Repo:     repoName,
			Username: username,
			Keyword:  keyword,
			Label:    label,
			Interval: interval,
			OnComment: func(c githubx.Comment) {
				if strings.TrimSpace(c.Body) != "xagent fix" {
					return
				}

				slog.Info("found fix command", "pr", c.PRNumber)

				links, err := xagent.FindLinksByURL(ctx, &xagentv1.FindLinksByURLRequest{Url: c.PRURL})
				if err != nil {
					slog.Error("failed to find links", "url", c.PRURL, "error", err)
					return
				}
				if len(links.Links) == 0 {
					slog.Info("no tasks linked to PR", "url", c.PRURL)
					return
				}

				taskID := links.Links[0].TaskId
				_, err = xagent.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
					Id:     taskID,
					Status: "pending",
					AddPrompts: []string{
						fmt.Sprintf("The following linked PR contains comments indicating that it needs to be fixed: %s", c.PRURL),
					},
				})
				if err != nil {
					slog.Error("failed to update task", "task", taskID, "error", err)
					return
				}

				slog.Info("task updated", "task", taskID)
			},
		})

		return poller.Run(ctx)
	},
}

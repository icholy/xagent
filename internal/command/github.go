package command

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
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
		&cli.StringFlag{
			Name:    "token",
			Usage:   "GitHub token",
			Sources: cli.EnvVars("GITHUB_TOKEN"),
		},
		&cli.StringFlag{
			Name:    "data",
			Aliases: []string{"d"},
			Usage:   "Data directory for state persistence",
			Value:   "data",
		},
		&cli.StringFlag{
			Name:     "workspace",
			Aliases:  []string{"w"},
			Usage:    "Workspace for new tasks",
			Required: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		repo := cmd.String("repo")
		username := cmd.String("username")
		label := cmd.String("label")
		interval := cmd.Duration("interval")
		serverURL := cmd.String("server")
		dataDir := cmd.String("data")
		workspace := cmd.String("workspace")

		parts := strings.SplitN(repo, "/", 2)
		if len(parts) != 2 {
			return cli.Exit("repo must be in owner/repo format", 1)
		}
		owner, repoName := parts[0], parts[1]

		ghClient := github.NewClient(nil)
		if token := cmd.String("token"); token != "" {
			ghClient = ghClient.WithAuthToken(token)
		}

		xagent := xagentclient.New(serverURL)

		slog.Info("starting github poller",
			"repo", repo,
			"username", username,
			"label", label,
			"interval", interval,
		)

		poller := githubx.NewPoller(githubx.PollerOptions{
			Client:    ghClient,
			Owner:     owner,
			Repo:      repoName,
			Username:  username,
			Label:     label,
			Interval:  interval,
			StateFile: filepath.Join(dataDir, "github.json"),
			OnComment: func(c githubx.Comment) {
				body := strings.TrimSpace(c.Body)

				reply := func(msg string) {
					_, _, err := ghClient.Issues.CreateComment(ctx, owner, repoName, c.PRNumber, &github.IssueComment{Body: &msg})
					if err != nil {
						slog.Error("failed to reply", "error", err)
					}
				}

				switch {
				case strings.HasPrefix(body, "xagent task"):
					eventResp, err := xagent.CreateEvent(ctx, &xagentv1.CreateEventRequest{
						Description: body,
						Url:         c.PRURL,
					})
					if err != nil {
						slog.Error("failed to create event", "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					processResp, err := xagent.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
						Id: eventResp.Event.Id,
					})
					if err != nil {
						slog.Error("failed to process event", "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					if len(processResp.TaskIds) == 0 {
						slog.Info("no tasks linked to PR", "url", c.PRURL)
						reply("error: no tasks linked to this PR")
						return
					}
					slog.Info("event processed", "event", eventResp.Event.Id, "tasks", processResp.TaskIds)

				case strings.HasPrefix(body, "xagent new"):
					resp, err := xagent.CreateTask(ctx, &xagentv1.CreateTaskRequest{
						Workspace: workspace,
						Instructions: []*xagentv1.Instruction{
							{Text: body, Url: c.PRURL},
						},
					})
					if err != nil {
						slog.Error("failed to create task", "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					taskID := resp.Task.Id
					_, err = xagent.CreateLink(ctx, &xagentv1.CreateLinkRequest{
						TaskId:    taskID,
						Relevance: "Task initiated from this PR",
						Url:       c.PRURL,
					})
					if err != nil {
						slog.Error("failed to create link", "error", err)
					}
					slog.Info("task created", "task", taskID)
				}
			},
		})

		return poller.Run(ctx)
	},
}

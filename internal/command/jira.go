package command

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	jira "github.com/andygrunwald/go-jira/v2/cloud"
	"github.com/icholy/xagent/internal/jirax"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var JiraCommand = &cli.Command{
	Name:  "jira",
	Usage: "Poll Jira for issue comments",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "label",
			Usage: "Label to filter issues by",
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
			Name:    "url",
			Usage:   "Jira base URL",
			Sources: cli.EnvVars("JIRA_BASE_URL"),
		},
		&cli.StringFlag{
			Name:    "username",
			Aliases: []string{"u"},
			Usage:   "Jira username (email)",
			Sources: cli.EnvVars("JIRA_USERNAME"),
		},
		&cli.StringFlag{
			Name:    "token",
			Usage:   "Jira API token",
			Sources: cli.EnvVars("JIRA_API_TOKEN"),
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
		label := cmd.String("label")
		interval := cmd.Duration("interval")
		serverURL := cmd.String("server")
		jiraURL := cmd.String("url")
		username := cmd.String("username")
		token := cmd.String("token")
		dataDir := cmd.String("data")
		workspace := cmd.String("workspace")

		tp := jira.BasicAuthTransport{
			Username: username,
			APIToken: token,
		}

		jiraClient, err := jira.NewClient(jiraURL, tp.Client())
		if err != nil {
			return fmt.Errorf("failed to create jira client: %w", err)
		}

		done, err := jirax.StatusList(ctx, jiraClient, "Done")
		if err != nil {
			return fmt.Errorf("failed to get done statuses: %w", err)
		}

		xagent := xagentclient.New(serverURL)

		slog.Info("starting jira poller",
			"username", username,
			"label", label,
			"interval", interval,
			"num_done_statuses", len(done),
		)

		poller := jirax.NewPoller(jirax.PollerOptions{
			Client:   jiraClient,
			Username: username,
			JQL: jirax.JQL{
				Labels:    []string{label},
				NotStatus: done,
			},
			Interval:  interval,
			StateFile: filepath.Join(dataDir, "jira.json"),
			OnComment: func(c jirax.Comment) {
				body := strings.TrimSpace(c.Body)
				prompt := fmt.Sprintf("A comment was left at %s: %s", c.IssueURL, body)

				reply := func(msg string) {
					_, _, err := jiraClient.Issue.AddComment(ctx, c.IssueKey, &jira.Comment{Body: msg})
					if err != nil {
						slog.Error("failed to reply", "error", err)
					}
				}

				switch {
				case strings.HasPrefix(body, "xagent task"):
					links, err := xagent.FindLinksByURL(ctx, &xagentv1.FindLinksByURLRequest{Url: c.IssueURL})
					if err != nil {
						slog.Error("failed to find links", "url", c.IssueURL, "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					if len(links.Links) == 0 {
						slog.Info("no tasks linked to issue", "url", c.IssueURL)
						reply("error: no tasks linked to this issue")
						return
					}
					taskID := links.Links[0].TaskId
					_, err = xagent.UpdateTask(ctx, &xagentv1.UpdateTaskRequest{
						Id:         taskID,
						Status:     "pending",
						AddPrompts: []string{prompt},
					})
					if err != nil {
						slog.Error("failed to update task", "task", taskID, "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					reply(fmt.Sprintf("task updated: %s", taskID))
					slog.Info("task updated", "task", taskID)

				case strings.HasPrefix(body, "xagent new"):
					resp, err := xagent.CreateTask(ctx, &xagentv1.CreateTaskRequest{
						Workspace: workspace,
						Prompts:   []string{prompt},
					})
					if err != nil {
						slog.Error("failed to create task", "error", err)
						reply(fmt.Sprintf("error: %v", err))
						return
					}
					taskID := resp.Task.Id
					_, err = xagent.CreateLink(ctx, &xagentv1.CreateLinkRequest{
						TaskId: taskID,
						Type:   "jira",
						Url:    c.IssueURL,
					})
					if err != nil {
						slog.Error("failed to create link", "error", err)
					}
					reply(fmt.Sprintf("task created: %s", taskID))
					slog.Info("task created", "task", taskID)
				}
			},
		})

		return poller.Run(ctx)
	},
}

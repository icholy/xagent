package command

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/webhook"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

var SubscribeCommand = &cli.Command{
	Name:  "subscribe",
	Usage: "Subscribe to webhook events from SQS",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:     "queue-url",
			Aliases:  []string{"q"},
			Usage:    "SQS queue URL",
			Sources:  cli.EnvVars("SQS_QUEUE_URL"),
			Required: true,
		},
		&cli.IntFlag{
			Name:    "max-messages",
			Aliases: []string{"m"},
			Usage:   "Maximum number of messages to receive per poll",
			Value:   10,
		},
		&cli.DurationFlag{
			Name:    "wait-time",
			Aliases: []string{"w"},
			Usage:   "Wait time for long polling",
			Value:   20 * time.Second,
		},
		&cli.DurationFlag{
			Name:  "poll-interval",
			Usage: "Interval between polls when queue is empty",
			Value: 5 * time.Second,
		},
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "xagent server URL",
			Value:   "http://localhost:6464",
		},
		&cli.StringFlag{
			Name:    "region",
			Aliases: []string{"r"},
			Usage:   "AWS region",
			Sources: cli.EnvVars("AWS_REGION"),
		},
		&cli.StringFlag{
			Name:    "github-username",
			Usage:   "Only process GitHub events from this user",
			Sources: cli.EnvVars("GITHUB_USERNAME"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		queueURL := cmd.String("queue-url")
		maxMessages := int32(cmd.Int("max-messages"))
		waitTime := cmd.Duration("wait-time")
		pollInterval := cmd.Duration("poll-interval")
		serverURL := cmd.String("server")
		region := cmd.String("region")
		githubUsername := cmd.String("github-username")

		var opts []func(*config.LoadOptions) error
		if region != "" {
			opts = append(opts, config.WithRegion(region))
		}
		awsConfig, err := config.LoadDefaultConfig(ctx, opts...)
		if err != nil {
			return fmt.Errorf("failed to load AWS config: %w", err)
		}

		sqsClient := sqs.NewFromConfig(awsConfig)
		xagent := xagentclient.New(serverURL)

		handler := &xagentEventHandler{
			client:         xagent,
			githubUsername: githubUsername,
		}

		subscriber := webhook.NewSQSSubscriber(&webhook.SQSSubscriberConfig{
			Client:       sqsClient,
			QueueURL:     queueURL,
			MaxMessages:  maxMessages,
			WaitTime:     waitTime,
			PollInterval: pollInterval,
			Handler:      handler,
		})

		return subscriber.Run(ctx)
	},
}

type xagentEventHandler struct {
	client         xagentclient.Client
	githubUsername string
}

func (h *xagentEventHandler) HandleEvent(ctx context.Context, event *webhook.Event) error {
	// Filter by GitHub username if configured
	if h.githubUsername != "" && event.Sender != "" && event.Sender != h.githubUsername {
		slog.Debug("ignoring event from different sender",
			"sender", event.Sender,
			"expected", h.githubUsername,
			"url", event.URL,
		)
		return nil
	}

	eventResp, err := h.client.CreateEvent(ctx, &xagentv1.CreateEventRequest{
		Description: event.Description,
		Data:        event.Data,
		Url:         event.URL,
	})
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	processResp, err := h.client.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
		Id: eventResp.Event.Id,
	})
	if err != nil {
		return fmt.Errorf("failed to process event: %w", err)
	}

	if len(processResp.TaskIds) == 0 {
		slog.Warn("no tasks linked to URL", "url", event.URL)
		return nil
	}

	slog.Info("event processed", "event_id", eventResp.Event.Id, "task_ids", processResp.TaskIds)
	return nil
}

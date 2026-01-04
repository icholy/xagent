package command

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
)

type SQSEvent struct {
	Description string `json:"description"`
	Data        string `json:"data"`
	URL         string `json:"url"`
}

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
		&cli.IntFlag{
			Name:    "wait-time",
			Aliases: []string{"w"},
			Usage:   "Wait time in seconds for long polling",
			Value:   20,
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
			Value:   "http://localhost:8080",
		},
		&cli.StringFlag{
			Name:     "workspace",
			Aliases:  []string{"ws"},
			Usage:    "Workspace for new tasks",
			Required: true,
		},
		&cli.StringFlag{
			Name:    "region",
			Aliases: []string{"r"},
			Usage:   "AWS region",
			Sources: cli.EnvVars("AWS_REGION"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		queueURL := cmd.String("queue-url")
		maxMessages := int32(cmd.Int("max-messages"))
		waitTime := int32(cmd.Int("wait-time"))
		pollInterval := cmd.Duration("poll-interval")
		serverURL := cmd.String("server")
		workspace := cmd.String("workspace")
		region := cmd.String("region")

		// Load AWS config
		var cfg config.LoadOptionsFunc
		if region != "" {
			cfg = config.WithRegion(region)
		}
		awsConfig, err := config.LoadDefaultConfig(ctx, cfg)
		if err != nil {
			return fmt.Errorf("failed to load AWS config: %w", err)
		}

		sqsClient := sqs.NewFromConfig(awsConfig)
		xagent := xagentclient.New(serverURL)

		slog.Info("starting SQS subscriber",
			"queue_url", queueURL,
			"max_messages", maxMessages,
			"wait_time", waitTime,
			"workspace", workspace,
		)

		for {
			// Receive messages from SQS
			result, err := sqsClient.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
				QueueUrl:            &queueURL,
				MaxNumberOfMessages: maxMessages,
				WaitTimeSeconds:     waitTime,
				VisibilityTimeout:   60, // 60 seconds to process message
			})
			if err != nil {
				slog.Error("failed to receive messages from SQS", "error", err)
				time.Sleep(pollInterval)
				continue
			}

			if len(result.Messages) == 0 {
				slog.Debug("no messages received, continuing to poll")
				continue
			}

			slog.Info("received messages", "count", len(result.Messages))

			for _, msg := range result.Messages {
				if err := processMessage(ctx, xagent, workspace, msg); err != nil {
					slog.Error("failed to process message", "error", err, "message_id", *msg.MessageId)
					// Don't delete the message so it can be retried
					continue
				}

				// Delete message from queue
				_, err := sqsClient.DeleteMessage(ctx, &sqs.DeleteMessageInput{
					QueueUrl:      &queueURL,
					ReceiptHandle: msg.ReceiptHandle,
				})
				if err != nil {
					slog.Error("failed to delete message from SQS", "error", err, "message_id", *msg.MessageId)
				} else {
					slog.Info("message processed and deleted", "message_id", *msg.MessageId)
				}
			}
		}
	},
}

func processMessage(ctx context.Context, xagent xagentclient.Client, workspace string, msg types.Message) error {
	// Parse event from message body
	var event SQSEvent
	if err := json.Unmarshal([]byte(*msg.Body), &event); err != nil {
		return fmt.Errorf("failed to parse event: %w", err)
	}

	slog.Info("processing event", "url", event.URL, "description", event.Description)

	body := strings.TrimSpace(event.Description)

	switch {
	case strings.HasPrefix(body, "xagent task"):
		// Create event and process it
		eventResp, err := xagent.CreateEvent(ctx, &xagentv1.CreateEventRequest{
			Description: event.Description,
			Data:        event.Data,
			Url:         event.URL,
		})
		if err != nil {
			return fmt.Errorf("failed to create event: %w", err)
		}

		processResp, err := xagent.ProcessEvent(ctx, &xagentv1.ProcessEventRequest{
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

	case strings.HasPrefix(body, "xagent new"):
		// Create new task
		resp, err := xagent.CreateTask(ctx, &xagentv1.CreateTaskRequest{
			Workspace: workspace,
			Instructions: []*xagentv1.Instruction{
				{Text: event.Description, Url: event.URL},
			},
		})
		if err != nil {
			return fmt.Errorf("failed to create task: %w", err)
		}

		taskID := resp.Task.Id

		// Create link to the source URL
		_, err = xagent.CreateLink(ctx, &xagentv1.CreateLinkRequest{
			TaskId:    taskID,
			Relevance: "Task initiated from this event",
			Url:       event.URL,
			Notify:    true,
		})
		if err != nil {
			slog.Error("failed to create link", "error", err, "task_id", taskID)
		}

		slog.Info("task created", "task_id", taskID, "workspace", workspace)

	default:
		slog.Warn("unknown command prefix", "description", event.Description)
	}

	return nil
}

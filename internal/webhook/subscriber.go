package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
)

// EventHandler processes webhook events.
type EventHandler interface {
	HandleEvent(ctx context.Context, event *Event) error
}

// SQSSubscriberConfig holds configuration for the SQS subscriber.
type SQSSubscriberConfig struct {
	Client       *sqs.Client
	QueueURL     string
	MaxMessages  int32
	WaitTime     time.Duration
	PollInterval time.Duration
	Handler      EventHandler
}

// SQSSubscriber subscribes to webhook events from an SQS queue.
type SQSSubscriber struct {
	config *SQSSubscriberConfig
}

// NewSQSSubscriber creates a new SQS subscriber.
func NewSQSSubscriber(config *SQSSubscriberConfig) *SQSSubscriber {
	return &SQSSubscriber{config: config}
}

// Run starts the subscriber loop and blocks until the context is canceled.
func (s *SQSSubscriber) Run(ctx context.Context) error {
	slog.Info("starting SQS subscriber",
		"queue_url", s.config.QueueURL,
		"max_messages", s.config.MaxMessages,
		"wait_time", s.config.WaitTime,
		"poll_interval", s.config.PollInterval,
	)

	for {
		result, err := s.config.Client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            &s.config.QueueURL,
			MaxNumberOfMessages: s.config.MaxMessages,
			WaitTimeSeconds:     int32(s.config.WaitTime.Seconds()),
			VisibilityTimeout:   60,
		})
		if err != nil {
			slog.Error("failed to receive messages from SQS", "error", err)
			time.Sleep(s.config.PollInterval)
			continue
		}

		if len(result.Messages) == 0 {
			slog.Debug("no messages received, continuing to poll")
			continue
		}

		slog.Info("received messages", "count", len(result.Messages))

		for _, msg := range result.Messages {
			var event Event
			if err := json.Unmarshal([]byte(*msg.Body), &event); err != nil {
				slog.Error("failed to parse event", "error", err, "message_id", *msg.MessageId)
				continue
			}

			slog.Info("processing event", "url", event.URL, "description", event.Description)

			if err := s.config.Handler.HandleEvent(ctx, &event); err != nil {
				slog.Error("failed to handle event", "error", err, "message_id", *msg.MessageId)
				continue
			}

			_, err := s.config.Client.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      &s.config.QueueURL,
				ReceiptHandle: msg.ReceiptHandle,
			})
			if err != nil {
				slog.Error("failed to delete message from SQS", "error", err, "message_id", *msg.MessageId)
			} else {
				slog.Info("message processed and deleted", "message_id", *msg.MessageId)
			}
		}
	}
}

// LogOnlyHandler is an EventHandler that only logs events (useful for testing).
type LogOnlyHandler struct{}

// HandleEvent logs the event without taking action.
func (h *LogOnlyHandler) HandleEvent(ctx context.Context, event *Event) error {
	slog.Info("received command", "event", event, "url")
	return nil
}

// FuncHandler adapts a function to the EventHandler interface.
type FuncHandler func(ctx context.Context, event *Event) error

// HandleEvent calls the underlying function.
func (f FuncHandler) HandleEvent(ctx context.Context, event *Event) error {
	return f(ctx, event)
}

// ValidateEvent checks if an event has the required fields.
func ValidateEvent(event *Event) error {
	if event.Description == "" {
		return fmt.Errorf("event description is required")
	}
	if event.URL == "" {
		return fmt.Errorf("event URL is required")
	}
	return nil
}

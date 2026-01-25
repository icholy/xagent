package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/icholy/xagent/internal/deviceauth"
	xagentv1 "github.com/icholy/xagent/internal/proto/xagent/v1"
	"github.com/icholy/xagent/internal/xagentclient"
	"github.com/urfave/cli/v3"
	"google.golang.org/protobuf/encoding/protojson"
)

var TaskListCommand = &cli.Command{
	Name:  "list",
	Usage: "List tasks from the server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "server",
			Aliases: []string{"s"},
			Usage:   "C2 server URL",
			Value:   "http://localhost:6464",
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
		&cli.StringFlag{
			Name:    "token-file",
			Usage:   "Path to authentication token file",
			Value:   "data/token.json",
			Sources: cli.EnvVars("XAGENT_TOKEN_FILE"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverURL := cmd.String("server")
		auth, err := deviceauth.New(deviceauth.Options{
			DiscoveryURL: deviceauth.DiscoveryURL(serverURL),
			TokenFile:    cmd.String("token-file"),
		})
		if err != nil {
			return fmt.Errorf("failed to initialize auth: %w", err)
		}
		client := xagentclient.New(serverURL, auth)

		resp, err := client.ListTasks(ctx, &xagentv1.ListTasksRequest{})
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		marshalOpts := protojson.MarshalOptions{Indent: "  "}

		// Get detailed information for each task and flatten the output
		result := make([]map[string]any, 0, len(resp.Tasks))
		for _, task := range resp.Tasks {
			details, err := client.GetTaskDetails(ctx, &xagentv1.GetTaskDetailsRequest{
				Id: task.Id,
			})
			if err != nil {
				return fmt.Errorf("failed to get details for task %d: %w", task.Id, err)
			}

			// Marshal nested arrays using protojson
			instructions := make([]json.RawMessage, len(details.Task.Instructions))
			for i, inst := range details.Task.Instructions {
				instructions[i], _ = marshalOpts.Marshal(inst)
			}

			links := make([]json.RawMessage, len(details.GetLinks()))
			for i, link := range details.GetLinks() {
				links[i], _ = marshalOpts.Marshal(link)
			}

			events := make([]json.RawMessage, len(details.GetEvents()))
			for i, event := range details.GetEvents() {
				events[i], _ = marshalOpts.Marshal(event)
			}

			children := make([]json.RawMessage, len(details.GetChildren()))
			for i, child := range details.GetChildren() {
				children[i], _ = marshalOpts.Marshal(child)
			}

			// Create flattened structure
			result = append(result, map[string]any{
				"id":           details.Task.Id,
				"name":         details.Task.Name,
				"status":       details.Task.Status,
				"instructions": instructions,
				"links":        links,
				"events":       events,
				"children":     children,
			})
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

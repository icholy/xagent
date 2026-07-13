package command

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/icholy/xagent/internal/configfile"
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
			Usage:   "server URL",
			Value:   xagentclient.DefaultURL,
			Sources: cli.EnvVars("XAGENT_SERVER"),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		serverURL := cmd.String("server")
		cfg, err := configfile.Load(nil)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		if cfg.Token == "" {
			return fmt.Errorf("not authenticated, run setup first")
		}
		client := xagentclient.New(xagentclient.Options{BaseURL: serverURL, Token: cfg.Token})

		resp, err := client.ListTasks(ctx, &xagentv1.ListTasksRequest{})
		if err != nil {
			return fmt.Errorf("failed to list tasks: %w", err)
		}

		// ListTasks already returns the fat Task per row, so render the header
		// straight from the response — no per-task detail fetch.
		marshalOpts := protojson.MarshalOptions{Indent: "  "}
		result := make([]json.RawMessage, len(resp.Tasks))
		for i, task := range resp.Tasks {
			result[i], err = marshalOpts.Marshal(task)
			if err != nil {
				return fmt.Errorf("failed to marshal task %d: %w", task.Id, err)
			}
		}

		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	},
}

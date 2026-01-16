package command

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/icholy/xagent/internal/server"
	"github.com/icholy/xagent/internal/store"
	"github.com/urfave/cli/v3"
)

var ServerCommand = &cli.Command{
	Name:  "server",
	Usage: "Start the xagent server",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:    "addr",
			Aliases: []string{"a"},
			Usage:   "Address to listen on",
			Value:   ":6464",
		},
		&cli.StringFlag{
			Name:    "db",
			Aliases: []string{"d"},
			Usage:   "Database file path",
			Value:   "data/xagent.db",
		},
		&cli.BoolFlag{
			Name:  "notify",
			Usage: "Send system notification when a task finishes",
			Value: true,
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		addr := cmd.String("addr")
		dbPath := cmd.String("db")
		notifyFlag := cmd.Bool("notify")

		db, err := store.Open(dbPath)
		if err != nil {
			return fmt.Errorf("failed to open database: %w", err)
		}
		defer db.Close()

		tasks := store.NewTaskRepository(db)
		logs := store.NewLogRepository(db)
		links := store.NewLinkRepository(db)
		events := store.NewEventRepository(db)
		workspaces := store.NewWorkspaceRepository(db)
		srv := server.New(server.Options{
			Tasks:      tasks,
			Logs:       logs,
			Links:      links,
			Events:     events,
			Workspaces: workspaces,
			Notify:     notifyFlag,
		})

		slog.Info("starting server", "addr", addr, "db", dbPath)
		if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	},
}

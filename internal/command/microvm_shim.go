package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/icholy/xagent/internal/microvmshim"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
	"github.com/urfave/cli/v3"
)

// MicrovmShimCommand is the application entrypoint baked into Lambda MicroVMs
// images. It serves the AWS lifecycle hooks plus the xagent control surface,
// fetches the task's spec bundle on /run, (re-)spawns and supervises the driver,
// and reports the driver's exit over the /xagent/lifecycle SSE stream. It holds
// NO AWS credentials and makes NO control-plane calls — suspend/resume/terminate
// are the runner's. See the lambdamicrovm backend.
var MicrovmShimCommand = &cli.Command{
	Name:  "microvm-shim",
	Usage: "Run the in-MicroVM lifecycle-hook server (Lambda MicroVMs image entrypoint)",
	Flags: []cli.Flag{
		&cli.StringFlag{
			Name:  "addr",
			Usage: "Address to serve the lifecycle hooks on",
			Value: fmt.Sprintf(":%d", awsmicrovm.DefaultPort),
		},
	},
	Action: func(ctx context.Context, cmd *cli.Command) error {
		srv := &microvmshim.Server{Log: slog.Default()}
		slog.Info("microvm-shim listening", "addr", cmd.String("addr"))
		return srv.ListenAndServe(ctx, cmd.String("addr"))
	},
}

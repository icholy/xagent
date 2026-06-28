package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/icholy/xagent/internal/microvmshim"
	"github.com/icholy/xagent/internal/x/awsmicrovm"
	"github.com/urfave/cli/v3"
)

// MicrovmShimCommand is the application entrypoint baked into Lambda MicroVMs
// images. It serves the MicroVM lifecycle hooks, fetches the task's spec bundle
// on /run, and supervises the driver. See the lambdamicrovm backend.
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
		// Self-termination on driver exit (and the /terminate fallback) needs the
		// AWS client, which uses the MicroVM's execution-role credentials and
		// region (AWS_REGION) from the standard SDK chain. If the config can't be
		// loaded the shim still serves hooks but cannot terminate the VM, leaving
		// --maximum-duration-in-seconds as the backstop.
		if cfg, err := config.LoadDefaultConfig(ctx); err != nil {
			slog.Warn("no AWS config; microvm cannot self-terminate", "error", err)
		} else {
			client := awsmicrovm.NewClient(cfg)
			srv.Terminate = func(ctx context.Context, microvmID string) error {
				_, err := client.TerminateMicrovm(ctx, &awsmicrovm.TerminateMicrovmInput{MicrovmID: microvmID})
				return err
			}
		}

		slog.Info("microvm-shim listening", "addr", cmd.String("addr"))
		return srv.ListenAndServe(ctx, cmd.String("addr"))
	},
}

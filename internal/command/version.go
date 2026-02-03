package command

import (
	"context"
	"fmt"
	"runtime/debug"

	"github.com/urfave/cli/v3"
)

var VersionCommand = &cli.Command{
	Name:  "version",
	Usage: "Print the version",
	Action: func(ctx context.Context, cmd *cli.Command) error {
		info, ok := debug.ReadBuildInfo()
		if !ok {
			fmt.Println("(unknown)")
			return nil
		}
		version := info.Main.Version
		if version == "" || version == "(devel)" {
			// Try to get VCS info from build settings
			for _, setting := range info.Settings {
				if setting.Key == "vcs.revision" {
					version = setting.Value
					if len(version) > 8 {
						version = version[:8]
					}
					break
				}
			}
		}
		if version == "" {
			version = "(devel)"
		}
		fmt.Println(version)
		return nil
	},
}

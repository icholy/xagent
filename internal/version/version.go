package version

import "runtime/debug"

// Version is set via -ldflags at build time for releases. When set, it takes
// precedence over the information from debug.ReadBuildInfo().
var Version string

// String returns the build version. It prefers the version injected at build
// time, then the module version, falling back to the short VCS revision for
// local builds and "(devel)" / "(unknown)" when no version information is
// available.
func String() string {
	if Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "(unknown)"
	}
	version := info.Main.Version
	if version == "" || version == "(devel)" {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				v := setting.Value
				if len(v) > 8 {
					v = v[:8]
				}
				return v
			}
		}
	}
	if version == "" {
		return "(devel)"
	}
	return version
}

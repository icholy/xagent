package version

import "runtime/debug"

// String returns the build version. It prefers the module version when
// available, falling back to the short VCS revision for local builds and
// "(devel)" / "(unknown)" when no version information is available.
func String() string {
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

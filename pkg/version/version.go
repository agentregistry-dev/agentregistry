package version

import "strings"

var (
	// Version identifies the application release.
	Version = "dev"
	// GitCommit identifies the source revision used for the build.
	GitCommit = "unknown"
	// BuildDate identifies when the application was built.
	BuildDate = "unknown"
)

// EnsureVPrefix adds a "v" prefix if missing; golang.org/x/mod/semver requires it.
func EnsureVPrefix(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}

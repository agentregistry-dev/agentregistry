package version

import "strings"

var (
	Version        = "dev"
	GitCommit      = "unknown"
	BuildDate      = "unknown"
	DockerRegistry = "localhost:5001"
)

// EnsureVPrefix adds a "v" prefix if missing; golang.org/x/mod/semver requires it.
func EnsureVPrefix(s string) string {
	if strings.HasPrefix(s, "v") {
		return s
	}
	return "v" + s
}

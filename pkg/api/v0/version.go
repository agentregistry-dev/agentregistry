package v0

// VersionBody represents API version information.
type VersionBody struct {
	Version   string `json:"version" example:"v1.0.0" doc:"Application version"`
	GitCommit string `json:"git_commit" example:"abc123d" doc:"Git commit SHA"`
	BuildTime string `json:"build_time" example:"2025-10-14T12:00:00Z" doc:"Build timestamp"`
}

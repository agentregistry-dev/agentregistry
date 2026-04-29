package annotations

const (
	// AnnotationSkipTokenResolution skips CLI token resolution during pre-run.
	// The command still gets an API client, just without running token resolution.
	AnnotationSkipTokenResolution = "skipTokenResolution"

	// AnnotationOptionalRegistry marks a command as tolerant of an unreachable
	// registry or unresolvable auth token. Pre-run still runs (so flags, env,
	// and the OIDC token provider are honored), but failures during token
	// resolution or the client connectivity check are soft-failed: the command
	// still gets a client and may handle errors itself.
	AnnotationOptionalRegistry = "optionalRegistry"
)

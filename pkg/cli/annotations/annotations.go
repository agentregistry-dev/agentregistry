package annotations

const (
	// AnnotationSkipTokenResolution skips CLI token resolution during pre-run.
	// The command still gets an API client, just without running token resolution.
	AnnotationSkipTokenResolution = "skipTokenResolution"

	// AnnotationOptionalRegistry marks a command as tolerant of an unreachable
	// registry during client setup. Pre-run still runs (so flags, env, and the
	// OIDC token provider are honored), but only client creation/connectivity
	// failures are soft-failed: the command still gets a client and may handle
	// those errors itself. Token resolution failures are still returned.
	AnnotationOptionalRegistry = "optionalRegistry"
)

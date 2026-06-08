package types

// Default runner image refs for non-OCI MCPPackage origins. Exported so
// out-of-tree consumers reuse the same values rather than maintaining a
// parallel copy.
const (
	DefaultNPMRunnerImage  = "node:24-alpine3.21"
	DefaultPyPIRunnerImage = "ghcr.io/astral-sh/uv:debian"
)

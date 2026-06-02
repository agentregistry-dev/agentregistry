package registries

// Canonical public registry base URLs the validators fall back to when
// MCPPackageOriginNPM.Mirror / MCPPackageOriginPyPI.Mirror is empty.
// These are validator-side concerns: the API types in pkg/api/v1alpha1
// don't reference them. Operators retargeting the validators at a
// private mirror (Verdaccio for npm, devpi for PyPI) only touch this
// file. An explicit Mirror on a package is honored as-is and used to
// drive the upstream HTTP probe; non-empty values are treated as
// overrides, not violations.
const (
	DefaultURLNPM  = "https://registry.npmjs.org"
	DefaultURLPyPI = "https://pypi.org"
)

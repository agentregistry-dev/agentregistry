package registries

// Canonical public registry base URLs the validators fall back to when
// MCPPackage.RegistryBaseURL is empty. These are validator-side
// concerns: the API types in pkg/api/v1alpha1 don't reference them and
// don't care which registry a package was sourced from. Keeping them
// here (rather than in pkg/api/v1alpha1) keeps the wire-vocabulary
// (RegistryType*) and the validator-side defaults from drifting together
// — operators retargeting the validators at a private mirror only
// touch this file.
//
// Per @josh-pritchard on PR #449: an explicit RegistryBaseURL on a
// package is treated as an override, not a violation. The validators
// fall back to these defaults only when RegistryBaseURL is empty;
// any non-empty value is honored as-is and used to drive the upstream
// HTTP probe. That makes private mirrors (e.g. Verdaccio for npm,
// devpi for PyPI, an internal Artifactory for NuGet) work without
// patching the OSS code.
const (
	DefaultURLNPM   = "https://registry.npmjs.org"
	DefaultURLPyPI  = "https://pypi.org"
	DefaultURLNuGet = "https://api.nuget.org"
)

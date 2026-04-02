package service

import (
	"time"

	"github.com/agentregistry-dev/agentregistry/internal/registry/service/internal/versionutil"
)

// IsSemanticVersion checks if a version string follows semantic versioning format
// Uses the official golang.org/x/mod/semver package for validation
// Requires exactly three parts: major.minor.patch (optionally with prerelease/build)
func IsSemanticVersion(version string) bool {
	return versionutil.IsSemanticVersion(version)
}

// CompareVersions implements the versioning strategy agreed upon in the discussion:
// 1. If both versions are valid semver, use semantic version comparison
// 2. If neither are valid semver, use publication timestamp (return 0 to indicate equal for sorting)
// 3. If one is semver and one is not, the semver version is always considered higher
func CompareVersions(version1 string, version2 string, timestamp1 time.Time, timestamp2 time.Time) int {
	return versionutil.CompareVersions(version1, version2, timestamp1, timestamp2)
}

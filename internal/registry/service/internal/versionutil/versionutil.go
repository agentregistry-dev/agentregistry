package versionutil

import (
	"strings"
	"time"

	versionpkg "github.com/agentregistry-dev/agentregistry/internal/version"
	"golang.org/x/mod/semver"
)

func IsSemanticVersion(version string) bool {
	versionWithV := versionpkg.EnsureVPrefix(version)
	if !semver.IsValid(versionWithV) {
		return false
	}

	versionCore := strings.TrimPrefix(versionWithV, "v")
	if idx := strings.Index(versionCore, "-"); idx != -1 {
		versionCore = versionCore[:idx]
	}
	if idx := strings.Index(versionCore, "+"); idx != -1 {
		versionCore = versionCore[:idx]
	}

	parts := strings.Split(versionCore, ".")
	return len(parts) == 3
}

func CompareVersions(version1, version2 string, timestamp1, timestamp2 time.Time) int {
	isSemver1 := IsSemanticVersion(version1)
	isSemver2 := IsSemanticVersion(version2)

	if isSemver1 && isSemver2 {
		return semver.Compare(versionpkg.EnsureVPrefix(version1), versionpkg.EnsureVPrefix(version2))
	}

	if !isSemver1 && !isSemver2 {
		if timestamp1.Before(timestamp2) {
			return -1
		}
		if timestamp1.After(timestamp2) {
			return 1
		}
		return 0
	}

	if isSemver1 {
		return 1
	}
	return -1
}

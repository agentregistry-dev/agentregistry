package utils

import (
	"os/exec"
	"strings"
)

// SanitizeVersion sanitizes a version string for use in filesystem paths.
// Replaces invalid filesystem characters with hyphens.
func SanitizeVersion(version string) string {
	if version == "" {
		return ""
	}

	// Replace common invalid filesystem characters with hyphens
	sanitized := strings.ReplaceAll(version, "/", "-")
	sanitized = strings.ReplaceAll(sanitized, "\\", "-")
	sanitized = strings.ReplaceAll(sanitized, ":", "-")
	sanitized = strings.ReplaceAll(sanitized, "*", "-")
	sanitized = strings.ReplaceAll(sanitized, "?", "-")
	sanitized = strings.ReplaceAll(sanitized, "\"", "-")
	sanitized = strings.ReplaceAll(sanitized, "<", "-")
	sanitized = strings.ReplaceAll(sanitized, ">", "-")
	sanitized = strings.ReplaceAll(sanitized, "|", "-")
	// Remove leading/trailing dots and spaces
	sanitized = strings.Trim(sanitized, ". ")
	// Replace multiple consecutive hyphens with a single hyphen
	for strings.Contains(sanitized, "--") {
		sanitized = strings.ReplaceAll(sanitized, "--", "-")
	}
	return sanitized
}

func IsDockerComposeAvailable() bool {
	cmd := exec.Command("docker", "compose", "version")
	_, err := cmd.CombinedOutput()
	return err == nil
}

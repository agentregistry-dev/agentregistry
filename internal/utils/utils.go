package utils

import (
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"strings"
)

// FindAvailablePort finds an available port on localhost
func FindAvailablePort() (uint16, error) {
	// Try to bind to port 0, which tells the OS to pick an available port
	listener, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		return 0, fmt.Errorf("failed to find available port: %w", err)
	}
	defer func() { _ = listener.Close() }()

	// Get the port that was assigned
	addr := listener.Addr().(*net.TCPAddr)
	return uint16(addr.Port), nil
}

// ParseKeyValuePairs parses KEY=VALUE pairs.
func ParseKeyValuePairs(pairs []string) (map[string]string, error) {
	result := make(map[string]string)
	for _, pair := range pairs {
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid key=value pair (missing =): %s", pair)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("invalid key=value pair (empty key): %s", pair)
		}
		result[key] = value
	}
	return result, nil
}

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

var pythonIdentifierRegex = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

var pythonKeywords = map[string]struct{}{
	"False": {}, "None": {}, "True": {}, "and": {}, "as": {}, "assert": {},
	"async": {}, "await": {}, "break": {}, "class": {}, "continue": {}, "def": {},
	"del": {}, "elif": {}, "else": {}, "except": {}, "finally": {}, "for": {},
	"from": {}, "global": {}, "if": {}, "import": {}, "in": {}, "is": {},
	"lambda": {}, "nonlocal": {}, "not": {}, "or": {}, "pass": {}, "raise": {},
	"return": {}, "try": {}, "while": {}, "with": {}, "yield": {},
}

// ValidatePythonIdentifier checks if the given name is a valid Python identifier.
// It returns an error if the name doesn't match Python identifier rules or is a reserved keyword.
func ValidatePythonIdentifier(name string) error {
	if !pythonIdentifierRegex.MatchString(name) {
		return fmt.Errorf("%q is not a valid Python identifier: must start with a letter or underscore and contain only letters, digits, and underscores", name)
	}
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return fmt.Errorf("%q is a Python keyword and cannot be used", name)
	}
	return nil
}

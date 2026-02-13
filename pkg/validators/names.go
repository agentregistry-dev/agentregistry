// Package validators provides centralized validation functions for resource names
// across the AgentRegistry CLI and services.
package validators

import (
	"fmt"
	"regexp"
	"strings"
)

// Name validation patterns
var (
	// namespaceRegex validates the namespace part of a server name
	// - Must start and end with alphanumeric
	// - Can contain dots and hyphens in the middle
	// - Minimum 2 characters
	namespaceRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9.-]*[a-zA-Z0-9]$`)

	// serverNamePartRegex validates the name part of a server name
	// - Must start and end with alphanumeric
	// - Can contain dots, underscores, and hyphens in the middle
	// - Minimum 2 characters
	serverNamePartRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$`)

	// skillNameRegex matches the database constraint for skill names
	// - Can contain alphanumeric, underscores, and hyphens
	skillNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// Python keywords that cannot be used as agent names
var pythonKeywords = map[string]struct{}{
	"False": {}, "None": {}, "True": {}, "and": {}, "as": {}, "assert": {},
	"async": {}, "await": {}, "break": {}, "class": {}, "continue": {}, "def": {},
	"del": {}, "elif": {}, "else": {}, "except": {}, "finally": {}, "for": {},
	"from": {}, "global": {}, "if": {}, "import": {}, "in": {}, "is": {},
	"lambda": {}, "nonlocal": {}, "not": {}, "or": {}, "pass": {}, "raise": {},
	"return": {}, "try": {}, "while": {}, "with": {}, "yield": {},
}

// ValidateProjectName checks if the provided project name is valid for use as a directory name.
// This is a permissive check for filesystem safety.
func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}

	// Check for invalid characters
	if strings.ContainsAny(name, " \t\n\r/\\:*?\"<>|") {
		return fmt.Errorf("project name contains invalid characters")
	}

	// Check if it starts with a dot
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("project name cannot start with a dot")
	}

	return nil
}

// agentNameRegex enforces the strictest rule - names that work BOTH as Python identifiers AND as publishable agent names.
// Must start with a letter, followed by alphanumeric only, minimum 2 characters.
var agentNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9]+$`)

// ValidateAgentName checks if the agent name is valid.
// Allowed: letters and digits only, must start with a letter, minimum 2 characters.
// Not allowed: underscores, dots, hyphens, or Python keywords.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	if !agentNameRegex.MatchString(name) {
		return fmt.Errorf("agent name must start with a letter and contain only letters and digits (minimum 2 characters)")
	}

	// Reject Python keywords to avoid issues in generated code
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return fmt.Errorf("agent name %q is a Python keyword and cannot be used", name)
	}

	return nil
}

// ValidateMCPServerName checks if the MCP server name matches the required format.
// Server name must be in format "namespace/name" where:
// - namespace: starts/ends with alphanumeric, can contain dots and hyphens, min 2 chars
// - name: starts/ends with alphanumeric, can contain dots, underscores, and hyphens, min 2 chars
func ValidateMCPServerName(name string) error {
	if name == "" {
		return fmt.Errorf("server name cannot be empty")
	}

	parts := strings.SplitN(name, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return fmt.Errorf("server name must be in format 'namespace/name'")
	}

	namespace, serverName := parts[0], parts[1]

	if len(namespace) < 2 {
		return fmt.Errorf("namespace must be at least 2 characters")
	}
	if len(serverName) < 2 {
		return fmt.Errorf("server name part must be at least 2 characters")
	}

	if !namespaceRegex.MatchString(namespace) {
		return fmt.Errorf("invalid namespace %q: must start and end with alphanumeric, can contain letters, numbers, dots (.), and hyphens (-)", namespace)
	}

	if !serverNamePartRegex.MatchString(serverName) {
		return fmt.Errorf("invalid server name %q: must start and end with alphanumeric, can contain letters, numbers, dots (.), underscores (_), and hyphens (-)", serverName)
	}

	return nil
}

// ValidateSkillName checks if the skill name matches the required format for registry storage.
// Skill names can contain alphanumeric characters, underscores, and hyphens.
func ValidateSkillName(name string) error {
	if name == "" {
		return fmt.Errorf("skill name cannot be empty")
	}
	if !skillNameRegex.MatchString(name) {
		return fmt.Errorf("invalid skill name %q: can only contain letters, numbers, underscores (_), and hyphens (-)", name)
	}
	return nil
}

// Package validators provides centralized validation functions for resource names
// across the AgentRegistry CLI and services.
package validators

import (
	"fmt"
	"regexp"
	"strings"
)

// skillNameRegex matches the database constraint for skill names
// - Can contain alphanumeric, underscores, and hyphens
var skillNameRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

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
		return fmt.Errorf("agent name must start with a letter and contain only letters and digits (no hyphens, underscores, or dots; minimum 2 characters)")
	}

	// Reject Python keywords to avoid issues in generated code
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return fmt.Errorf("agent name %q is a Python keyword and cannot be used", name)
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

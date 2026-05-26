// Package validators provides centralized validation functions for resource names
// across the AgentRegistry CLI and services.
package validators

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// agentNameRegex enforces the strictest rule - names that work BOTH as Python identifiers AND as publishable agent names.
// Must start with a lowercase letter, followed by lowercase alphanumeric only, minimum 2 characters.
var agentNameRegex = regexp.MustCompile(`^[a-z][a-z0-9]+$`)

// Python keywords that cannot be used as agent names — agent names become
// Python identifiers in generated code, so the CLI layer rejects them in
// addition to the DNS-1123 form.
var pythonKeywords = map[string]struct{}{
	"False": {}, "None": {}, "True": {}, "and": {}, "as": {}, "assert": {},
	"async": {}, "await": {}, "break": {}, "class": {}, "continue": {}, "def": {},
	"del": {}, "elif": {}, "else": {}, "except": {}, "finally": {}, "for": {},
	"from": {}, "global": {}, "if": {}, "import": {}, "in": {}, "is": {},
	"lambda": {}, "nonlocal": {}, "not": {}, "or": {}, "pass": {}, "raise": {},
	"return": {}, "try": {}, "while": {}, "with": {}, "yield": {},
}

// ValidateProjectName checks if the provided project name is valid for use as a directory name.
// This is a permissive check for filesystem safety, not a resource-name check.
func ValidateProjectName(name string) error {
	if name == "" {
		return fmt.Errorf("project name cannot be empty")
	}
	if strings.ContainsAny(name, " \t\n\r/\\:*?\"<>|") {
		return fmt.Errorf("project name contains invalid characters")
	}
	if strings.HasPrefix(name, ".") {
		return fmt.Errorf("project name cannot start with a dot")
	}
	return nil
}

// validateName applies the v1alpha1 DNS-1123 subdomain rule with a
// kind-aware error message so CLI users see "skill name must be..." rather
// than the generic backend error.
func validateName(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s name cannot be empty", kind)
	}
	if len(name) > v1alpha1.DNSSubdomainMaxLen {
		return fmt.Errorf("%s name %q is too long (max %d chars, got %d)", kind, name, v1alpha1.DNSSubdomainMaxLen, len(name))
	}
	if !v1alpha1.DNSSubdomainRegex.MatchString(name) {
		return fmt.Errorf("%s name %q must be DNS-1123 subdomain: lowercase alphanumeric, hyphens, and dots; start/end with alphanumeric; each dot-separated segment 1-63 chars", kind, name)
	}
	return nil
}

// ValidateAgentName checks if the agent name is valid.
// Allowed: lowercase letters and digits only, must start with a letter, minimum 2 characters.
// Not allowed: uppercase, underscores, dots, hyphens, or Python keywords.
func ValidateAgentName(name string) error {
	if name == "" {
		return fmt.Errorf("agent name cannot be empty")
	}

	if !agentNameRegex.MatchString(name) {
		return fmt.Errorf("agent name must start with a lowercase letter and contain only lowercase letters and digits (no hyphens, underscores, or dots; minimum 2 characters)")
	}

	// Reject Python keywords to avoid issues in generated code
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return fmt.Errorf("agent name %q is a Python keyword and cannot be used", name)
	}

	return nil
}

// ValidateSkillName enforces DNS-1123 label form.
func ValidateSkillName(name string) error {
	return validateName("skill", name)
}

// ValidatePromptName enforces DNS-1123 label form.
func ValidatePromptName(name string) error {
	return validateName("prompt", name)
}

// ValidateMCPServerName enforces DNS-1123 label form.
func ValidateMCPServerName(name string) error {
	return validateName("MCP server", name)
}

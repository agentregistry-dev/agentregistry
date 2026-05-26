// Package validators provides centralized validation functions for resource names
// across the AgentRegistry CLI and services.
package validators

import (
	"fmt"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

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

// validateDNSLabel applies the v1alpha1 DNS-1123 label rule with a
// kind-aware error message so CLI users see "agent name must be..." rather
// than the generic backend error.
func validateDNSLabel(kind, name string) error {
	if name == "" {
		return fmt.Errorf("%s name cannot be empty", kind)
	}
	if !v1alpha1.DNSLabelRegex.MatchString(name) {
		return fmt.Errorf("%s name %q must be DNS-1123 label: lowercase alphanumeric and hyphens, max %d chars, start/end with alphanumeric", kind, name, v1alpha1.DNSLabelMaxLen)
	}
	return nil
}

// ValidateAgentName enforces DNS-1123 label form and rejects Python keywords.
// Python keyword rejection is CLI-only — agent names become Python identifiers
// in generated code, but the registry's API doesn't care.
func ValidateAgentName(name string) error {
	if err := validateDNSLabel("agent", name); err != nil {
		return err
	}
	if _, isKeyword := pythonKeywords[name]; isKeyword {
		return fmt.Errorf("agent name %q is a Python keyword and cannot be used", name)
	}
	return nil
}

// ValidateSkillName enforces DNS-1123 label form.
func ValidateSkillName(name string) error {
	return validateDNSLabel("skill", name)
}

// ValidatePromptName enforces DNS-1123 label form.
func ValidatePromptName(name string) error {
	return validateDNSLabel("prompt", name)
}

// ValidateDeploymentName enforces DNS-1123 label form.
func ValidateDeploymentName(name string) error {
	return validateDNSLabel("deployment", name)
}

// ValidateMCPServerName enforces DNS-1123 label form.
func ValidateMCPServerName(name string) error {
	return validateDNSLabel("MCP server", name)
}

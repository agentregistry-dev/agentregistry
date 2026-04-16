package validators

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"
)

// insecureSkipMCPHostVerifyEnv is the name of the environment variable that, when
// set to "true", disables localhost/host-verification checks for MCP remote URLs.
// This is intended for local development only and should never be enabled in production.
const insecureSkipMCPHostVerifyEnv = "INSECURE_SKIP_MCP_HOST_VERIFY"

// shouldSkipMCPHostVerify reports whether MCP remote URL host verification should be
// bypassed based on the INSECURE_SKIP_MCP_HOST_VERIFY environment variable.
func shouldSkipMCPHostVerify() bool {
	return strings.EqualFold(os.Getenv(insecureSkipMCPHostVerifyEnv), "true")
}

var (
	// gitRepoURLRegex validates repository URLs for common git hosting providers
	// (GitHub, GitLab, Bitbucket, etc.) in the standard owner/repo format.
	gitRepoURLRegex = regexp.MustCompile(`^https?://(www\.)?(github\.com|gitlab\.com|bitbucket\.org)/[\w.-]+/[\w.-]+/?$`)
)

// ValidateRepoURL validates a repository URL for the specified source type.
// Returns a descriptive error if validation fails, nil if valid.
func ValidateRepoURL(source RepositorySource, rawURL string) error {
	if source != SourceGit {
		return fmt.Errorf("%w: source must be %q, got %q", ErrInvalidRepositoryURL, SourceGit, source)
	}
	if !gitRepoURLRegex.MatchString(rawURL) {
		return fmt.Errorf("%w: %s (expected https://github.com|gitlab.com|bitbucket.org/OWNER/REPO)", ErrInvalidRepositoryURL, rawURL)
	}
	return nil
}

// HasNoSpaces checks if a string contains no spaces
func HasNoSpaces(s string) bool {
	return !strings.Contains(s, " ")
}

// extractTemplateVariables extracts template variables from a URL string
// e.g., "http://{host}:{port}/mcp" returns ["host", "port"]
func extractTemplateVariables(url string) []string {
	re := regexp.MustCompile(`\{([^}]+)\}`)
	matches := re.FindAllStringSubmatch(url, -1)

	var variables []string
	for _, match := range matches {
		if len(match) > 1 {
			variables = append(variables, match[1])
		}
	}
	return variables
}

// replaceTemplateVariables replaces template variables with placeholder values for URL validation
func replaceTemplateVariables(rawURL string) string {
	// Replace common template variables with valid placeholder values for parsing
	templateReplacements := map[string]string{
		"{host}":     "example.com",
		"{port}":     "8080",
		"{path}":     "api",
		"{protocol}": "http",
		"{scheme}":   "http",
	}

	result := rawURL
	for placeholder, replacement := range templateReplacements {
		result = strings.ReplaceAll(result, placeholder, replacement)
	}

	// Handle any remaining {variable} patterns with generic placeholder
	re := regexp.MustCompile(`\{[^}]+\}`)
	result = re.ReplaceAllString(result, "placeholder")

	return result
}

// IsValidURL checks if a URL is in valid format (basic structure validation)
func IsValidURL(rawURL string) bool {
	// Replace template variables with placeholders for parsing
	testURL := replaceTemplateVariables(rawURL)

	// Parse the URL
	u, err := url.Parse(testURL)
	if err != nil {
		return false
	}

	// Check if scheme is present (http or https)
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}

	if u.Host == "" {
		return false
	}
	return true
}

// IsValidSubfolderPath checks if a subfolder path is valid
func IsValidSubfolderPath(path string) bool {
	// Empty path is valid (subfolder is optional)
	if path == "" {
		return true
	}

	// Must not start with / (must be relative)
	if strings.HasPrefix(path, "/") {
		return false
	}

	// Must not end with / (clean path format)
	if strings.HasSuffix(path, "/") {
		return false
	}

	// Check for valid path characters (alphanumeric, dash, underscore, dot, forward slash)
	validPathRegex := regexp.MustCompile(`^[a-zA-Z0-9\-_./]+$`)
	if !validPathRegex.MatchString(path) {
		return false
	}

	// Check that path segments are valid
	for segment := range strings.SplitSeq(path, "/") {
		// Disallow empty segments ("//"), current dir ("."), and parent dir ("..")
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}

	return true
}

// IsValidRemoteURL checks if a URL is valid for remotes (stricter than packages - no localhost allowed)
func IsValidRemoteURL(rawURL string) bool {
	// First check basic URL structure
	if !IsValidURL(rawURL) {
		return false
	}

	// Parse the URL to check for localhost restriction
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}

	// Reject localhost URLs for remotes (security/production concerns).
	// This check is bypassed when INSECURE_SKIP_MCP_HOST_VERIFY=true (dev-only).
	if !shouldSkipMCPHostVerify() {
		hostname := u.Hostname()
		if hostname == "localhost" || hostname == "127.0.0.1" || strings.HasSuffix(hostname, ".localhost") {
			return false
		}
	}

	return true
}

// IsValidTemplatedURL validates a URL with template variables against available variables
// For packages: validates that template variables reference package arguments or environment variables
// For remotes: disallows template variables entirely
func IsValidTemplatedURL(rawURL string, availableVariables []string, allowTemplates bool) bool {
	// First check basic URL structure
	if !IsValidURL(rawURL) {
		return false
	}

	// Extract template variables from URL
	templateVars := extractTemplateVariables(rawURL)

	// If no templates are found, it's a valid static URL
	if len(templateVars) == 0 {
		return true
	}

	// If templates are not allowed (e.g., for remotes), reject URLs with templates
	if !allowTemplates {
		return false
	}

	// Validate that all template variables are available
	availableSet := make(map[string]bool)
	for _, v := range availableVariables {
		availableSet[v] = true
	}

	for _, templateVar := range templateVars {
		if !availableSet[templateVar] {
			return false
		}
	}

	return true
}

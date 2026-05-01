package importer

import (
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

// GitHubRepoFor inspects obj's repository reference and, when the URL
// parses as a GitHub repository, returns its (owner, repo) split.
// Supported kinds: Agent, MCPServer, Skill. Everything else returns
// ok=false.
//
// Shared across scanners that only target GitHub (OSV, Scorecard,
// future repo-hygiene checks) so the repository-resolution path
// stays in one place.
func GitHubRepoFor(obj v1alpha1.Object) (owner, repo string, ok bool) {
	var url string
	switch v := obj.(type) {
	case *v1alpha1.Agent:
		if v.Spec.Source != nil && v.Spec.Source.Repository != nil {
			url = v.Spec.Source.Repository.URL
		}
	case *v1alpha1.MCPServer:
		if v.Spec.Repository != nil {
			url = v.Spec.Repository.URL
		}
	case *v1alpha1.Skill:
		if v.Spec.Source != nil && v.Spec.Source.Repository != nil {
			url = v.Spec.Source.Repository.URL
		}
	default:
		return "", "", false
	}
	if url == "" {
		return "", "", false
	}
	return parseGitHubRepo(url)
}

// parseGitHubRepo accepts https / https-with-.git / ssh / bare
// "owner/repo" URL forms and returns (owner, repo, ok). Unknown
// forms return ok=false.
func parseGitHubRepo(raw string) (string, string, bool) {
	raw = strings.TrimSpace(raw)
	raw = strings.TrimSuffix(raw, ".git")
	var path string
	switch {
	case strings.Contains(raw, "github.com/"):
		parts := strings.Split(raw, "github.com/")
		path = parts[len(parts)-1]
	case strings.Contains(raw, "github.com:"):
		parts := strings.Split(raw, "github.com:")
		path = parts[len(parts)-1]
	default:
		return "", "", false
	}
	segs := strings.Split(strings.Trim(path, "/"), "/")
	if len(segs) < 2 || segs[0] == "" || segs[1] == "" {
		return "", "", false
	}
	return segs[0], segs[1], true
}

package v1alpha1

import (
	"context"
	"fmt"
)

// UniqueRemoteURLsFunc reports whether url is already claimed by any row
// of the given kind other than excludeName. Implementations typically scan
// the kind's table via Store.FindReferrers against a JSONB containment
// fragment like `{"remotes":[{"url":"..."}]}`.
//
// namespace is the referring object's namespace, advisory: the default
// checker searches cross-namespace (matching legacy behavior from before
// namespaces existed), but implementations are free to scope by it.
//
// Returns non-nil error when a conflict exists; the error message should
// name the conflicting (kind, name). A nil function is a no-op on every
// (Object).ValidateUniqueRemoteURLs caller — callers that aren't wired
// with a checker simply skip the check.
type UniqueRemoteURLsFunc func(ctx context.Context, kind, namespace, url, excludeName string) error

// validateRemoteURLs runs check against every non-empty URL in urls,
// accumulating FieldErrors under pathPrefix (e.g. "spec.remotes").
// Mirrors validatePackages in registry_validate.go.
func validateRemoteURLs(
	ctx context.Context,
	check UniqueRemoteURLsFunc,
	urls []string,
	kind, namespace, excludeName, pathPrefix string,
) FieldErrors {
	if check == nil || len(urls) == 0 {
		return nil
	}
	var errs FieldErrors
	for i, u := range urls {
		if u == "" {
			continue
		}
		if err := check(ctx, kind, namespace, u, excludeName); err != nil {
			errs.Append(fmt.Sprintf("%s[%d].url", pathPrefix, i), err)
		}
	}
	return errs
}

// ValidateUniqueRemoteURLs on *Agent checks that no OTHER Agent claims
// any URL in spec.remotes. Same-name/different-version rows sharing a
// URL are allowed (expected for version bumps of the same object).
func (a *Agent) ValidateUniqueRemoteURLs(ctx context.Context, check UniqueRemoteURLsFunc) error {
	if check == nil || len(a.Spec.Remotes) == 0 {
		return nil
	}
	urls := make([]string, len(a.Spec.Remotes))
	for i, r := range a.Spec.Remotes {
		urls[i] = r.URL
	}
	errs := validateRemoteURLs(ctx, check, urls, KindAgent, a.Metadata.Namespace, a.Metadata.Name, "spec.remotes")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidateUniqueRemoteURLs on *MCPServer — same contract as Agent.
func (m *MCPServer) ValidateUniqueRemoteURLs(ctx context.Context, check UniqueRemoteURLsFunc) error {
	if check == nil || len(m.Spec.Remotes) == 0 {
		return nil
	}
	urls := make([]string, len(m.Spec.Remotes))
	for i, r := range m.Spec.Remotes {
		urls[i] = r.URL
	}
	errs := validateRemoteURLs(ctx, check, urls, KindMCPServer, m.Metadata.Namespace, m.Metadata.Name, "spec.remotes")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// ValidateUniqueRemoteURLs on *Skill — same contract as Agent.
func (s *Skill) ValidateUniqueRemoteURLs(ctx context.Context, check UniqueRemoteURLsFunc) error {
	if check == nil || len(s.Spec.Remotes) == 0 {
		return nil
	}
	urls := make([]string, len(s.Spec.Remotes))
	for i, r := range s.Spec.Remotes {
		urls[i] = r.URL
	}
	errs := validateRemoteURLs(ctx, check, urls, KindSkill, s.Metadata.Namespace, s.Metadata.Name, "spec.remotes")
	if len(errs) == 0 {
		return nil
	}
	return errs
}

// Prompt / Provider / Deployment have no Remotes — no-op.
func (p *Prompt) ValidateUniqueRemoteURLs(context.Context, UniqueRemoteURLsFunc) error   { return nil }
func (p *Provider) ValidateUniqueRemoteURLs(context.Context, UniqueRemoteURLsFunc) error { return nil }
func (d *Deployment) ValidateUniqueRemoteURLs(context.Context, UniqueRemoteURLsFunc) error {
	return nil
}

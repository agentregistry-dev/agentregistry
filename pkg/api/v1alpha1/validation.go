package v1alpha1

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

// Validation error sentinels. All validation errors are wrapped in a
// FieldError (see below) so callers can introspect the failing path.
var (
	ErrRequiredField       = errors.New("required field missing")
	ErrInvalidFormat       = errors.New("invalid format")
	ErrInvalidVersion      = errors.New("invalid version string")
	ErrInvalidURL          = errors.New("invalid url")
	ErrInvalidLabel        = errors.New("invalid label")
	ErrInvalidRef          = errors.New("invalid resource reference")
	ErrUnknownPlatform     = errors.New("unknown provider platform")
	ErrInvalidDesiredState = errors.New("invalid deployment desired state")
	// ErrDanglingRef is returned by ResolverFunc implementations when the
	// referenced resource does not exist. Tests + callers identify
	// dangling references via errors.Is(err, ErrDanglingRef).
	ErrDanglingRef = errors.New("referenced resource not found")
)

// FieldError pins a validation failure to a dot-path inside the object.
// Examples: "metadata.name", "spec.packages[0].identifier",
// "spec.mcpServers[2]".
type FieldError struct {
	Path  string
	Cause error
}

func (fe FieldError) Error() string {
	if fe.Path == "" {
		return fe.Cause.Error()
	}
	return fe.Path + ": " + fe.Cause.Error()
}

func (fe FieldError) Unwrap() error { return fe.Cause }

// FieldErrors is the accumulated result of a validation pass. A nil or
// empty FieldErrors means success. It satisfies error so callers can
// return it directly.
type FieldErrors []FieldError

func (fe FieldErrors) Error() string {
	if len(fe) == 0 {
		return ""
	}
	msgs := make([]string, 0, len(fe))
	for _, e := range fe {
		msgs = append(msgs, e.Error())
	}
	return strings.Join(msgs, "; ")
}

// Append records a new field error under pathPrefix+path. If cause is
// nil, it's a no-op.
func (fe *FieldErrors) Append(path string, cause error) {
	if cause == nil {
		return
	}
	*fe = append(*fe, FieldError{Path: path, Cause: cause})
}

// ResolverFunc resolves a ResourceRef to an existing object. It should
// return ErrDanglingRef if the referenced object isn't found. Other
// errors (DB failures, etc.) propagate as-is.
type ResolverFunc func(ctx context.Context, ref ResourceRef) error

// GetterFunc fetches a ResourceRef as a typed Object. It returns
// ErrDanglingRef when the referenced object is missing; other errors
// propagate as-is. Used by reconcilers / platform adapters that need
// the target's Spec (not just an existence check) — for example, the
// local adapter walking an AgentSpec.MCPServers entry to build
// agentgateway upstream config.
type GetterFunc func(ctx context.Context, ref ResourceRef) (Object, error)

// -----------------------------------------------------------------------------
// Format rules — regexes and constants shared across every kind's validator.
// -----------------------------------------------------------------------------

// namespaceRegex: DNS-label-friendly. Lowercase letters, digits, hyphens,
// dots. Must start and end with alphanumeric. 1-63 chars. Matches
// Kubernetes namespace naming conventions.
var namespaceRegex = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,61}[a-z0-9])?$`)

// nameRegex: resource name. More permissive than namespace — allows
// uppercase, underscores, slashes (to support DNS-subdomain-style names
// like "ai.exa/exa"). 1-255 chars. Must start and end with alphanumeric.
var nameRegex = regexp.MustCompile(`^[a-zA-Z0-9]([-a-zA-Z0-9._/]{0,253}[a-zA-Z0-9])?$`)

// labelKeyRegex: Kubernetes label key format (prefix/name, prefix optional).
// Values up to 63 chars with the same character rules.
var labelKeyRegex = regexp.MustCompile(`^([a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?/)?[a-zA-Z0-9]([-a-zA-Z0-9._]{0,61}[a-zA-Z0-9])?$`)
var labelValueRegex = regexp.MustCompile(`^([a-zA-Z0-9]([-a-zA-Z0-9._]{0,61}[a-zA-Z0-9])?)?$`)

// versionRangeRegex: detects version strings that look like ranges or
// wildcards rather than concrete versions. Pinned versions like "v1.2.3"
// or "1.2.3-beta.1" must NOT match.
var versionRangeRegex = regexp.MustCompile(`(\^|~|>=|<=|>|<|\|\||\*|,|\bx\b|\bX\b|\s)`)

const maxVersionLen = 255

// -----------------------------------------------------------------------------
// ObjectMeta validation — shared across every kind.
// -----------------------------------------------------------------------------

// ValidateObjectMeta checks the namespace/name/version format and label
// shape. Server-managed fields (Generation, CreatedAt, UpdatedAt,
// DeletionTimestamp) are ignored.
//
// Use this for kinds where multiple coexisting versions of the same
// (namespace, name) carry meaning — Agent, MCPServer, Skill, Prompt
// (publishable artifacts). For kinds whose versioning is semantically
// empty (Provider is a connection handle, Deployment is a runtime
// binding), call ValidateObjectMetaUnversioned instead so callers
// aren't forced to fabricate a placeholder version string.
func ValidateObjectMeta(m ObjectMeta) FieldErrors {
	errs := validateObjectMetaCommon(m)
	if err := validateVersion(m.Version); err != nil {
		errs.Append("metadata.version", err)
	}
	return errs
}

// ValidateObjectMetaUnversioned is ValidateObjectMeta minus the
// version-required check. Kinds whose identity is fully captured by
// (namespace, name) — Provider, Deployment — call this so users
// don't have to make up a placeholder version. The storage layer
// still requires a version string in the 3-tuple PK, but kinds opting
// in here treat it as opaque (typically the constant "1").
func ValidateObjectMetaUnversioned(m ObjectMeta) FieldErrors {
	return validateObjectMetaCommon(m)
}

func validateObjectMetaCommon(m ObjectMeta) FieldErrors {
	var errs FieldErrors

	switch {
	case m.Namespace == "":
		errs.Append("metadata.namespace", fmt.Errorf("%w", ErrRequiredField))
	case !namespaceRegex.MatchString(m.Namespace):
		errs.Append("metadata.namespace", fmt.Errorf("%w: %q", ErrInvalidFormat, m.Namespace))
	}

	switch {
	case m.Name == "":
		errs.Append("metadata.name", fmt.Errorf("%w", ErrRequiredField))
	case !nameRegex.MatchString(m.Name):
		errs.Append("metadata.name", fmt.Errorf("%w: %q", ErrInvalidFormat, m.Name))
	}

	for key, val := range m.Labels {
		if !labelKeyRegex.MatchString(key) {
			errs.Append("metadata.labels["+key+"]", fmt.Errorf("%w: key %q", ErrInvalidLabel, key))
		}
		if !labelValueRegex.MatchString(val) {
			errs.Append("metadata.labels["+key+"]", fmt.Errorf("%w: value %q", ErrInvalidLabel, val))
		}
	}

	return errs
}

// validateVersion enforces: required, length-bound, not the literal
// "latest", and not a range-looking string.
func validateVersion(v string) error {
	if v == "" {
		return fmt.Errorf("%w", ErrRequiredField)
	}
	if len(v) > maxVersionLen {
		return fmt.Errorf("%w: exceeds %d characters", ErrInvalidVersion, maxVersionLen)
	}
	if strings.EqualFold(v, "latest") {
		return fmt.Errorf("%w: cannot be literal %q — use an explicit version", ErrInvalidVersion, "latest")
	}
	if versionRangeRegex.MatchString(v) {
		return fmt.Errorf("%w: must be a pinned version, not a range or wildcard (%q)", ErrInvalidVersion, v)
	}
	return nil
}

// -----------------------------------------------------------------------------
// Shared field validators — URL, repository, ResourceRef, non-empty title.
// -----------------------------------------------------------------------------

// validateWebsiteURL: optional field. If present, must be absolute https.
func validateWebsiteURL(u string) error {
	if u == "" {
		return nil
	}
	parsed, err := url.Parse(u)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
	}
	if parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be https, got %q", ErrInvalidURL, parsed.Scheme)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: host is empty", ErrInvalidURL)
	}
	return nil
}

// validateTitle: optional; when set, must not be whitespace-only.
func validateTitle(title string) error {
	if title == "" {
		return nil
	}
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("%w: title must not be whitespace-only", ErrInvalidFormat)
	}
	return nil
}

// validateRepository: optional; when set, URL must parse as https.
func validateRepository(r *Repository) FieldErrors {
	var errs FieldErrors
	if r == nil {
		return errs
	}
	if r.URL != "" {
		if err := validateWebsiteURL(r.URL); err != nil {
			errs.Append("repository.url", err)
		}
	}
	return errs
}

// validateRef: ResourceRef structural checks. allowedKinds restricts which
// Kind values are valid in this reference context (empty = any).
func validateRef(r ResourceRef, allowedKinds ...string) FieldErrors {
	var errs FieldErrors
	if r.Kind == "" {
		errs.Append("kind", fmt.Errorf("%w", ErrRequiredField))
	} else if len(allowedKinds) > 0 {
		found := slices.Contains(allowedKinds, r.Kind)
		if !found {
			errs.Append("kind", fmt.Errorf("%w: kind %q not allowed here (expected one of %v)", ErrInvalidRef, r.Kind, allowedKinds))
		}
	}
	if r.Namespace != "" && !namespaceRegex.MatchString(r.Namespace) {
		errs.Append("namespace", fmt.Errorf("%w: %q", ErrInvalidFormat, r.Namespace))
	}
	if r.Name == "" {
		errs.Append("name", fmt.Errorf("%w", ErrRequiredField))
	} else if !nameRegex.MatchString(r.Name) {
		errs.Append("name", fmt.Errorf("%w: %q", ErrInvalidFormat, r.Name))
	}
	// Version is optional on a ref — blank means "resolve to latest".
	if r.Version != "" {
		if err := validateVersion(r.Version); err != nil {
			errs.Append("version", err)
		}
	}
	return errs
}

// resolveRefWith runs resolver against ref and prepends pathPrefix to any
// reported error. Returns a FieldErrors slice (one entry if resolver failed,
// empty otherwise) so callers can uniformly accumulate.
func resolveRefWith(ctx context.Context, resolver ResolverFunc, ref ResourceRef, pathPrefix string) FieldErrors {
	if resolver == nil {
		return nil
	}
	if err := resolver(ctx, ref); err != nil {
		return FieldErrors{{Path: pathPrefix, Cause: err}}
	}
	return nil
}

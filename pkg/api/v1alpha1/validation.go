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
	ErrInvalidTag          = errors.New("invalid tag")
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

var tagRegex = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}$`)

// -----------------------------------------------------------------------------
// ObjectMeta validation — shared across every kind.
// -----------------------------------------------------------------------------

// ValidateObjectMeta checks the namespace/name format and label shape.
// Server-managed fields (CreatedAt, UpdatedAt, DeletionTimestamp) are ignored.
// Content resources use metadata.tag for identity; mutable object kinds expose
// only namespace/name.
//
// Taggable artifact kinds and mutable object kinds call this same validator
// because ObjectMeta exposes one public shape for both identities.
func ValidateObjectMeta(m ObjectMeta) FieldErrors {
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

func validateTag(tag string) error {
	if tag == "" {
		return fmt.Errorf("%w", ErrRequiredField)
	}
	if !tagRegex.MatchString(tag) {
		return fmt.Errorf("%w: must match %s", ErrInvalidTag, tagRegex.String())
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

// validateRepository validates the Repository object
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
	// Tag is optional on content refs — blank means "resolve to latest".
	if r.Tag != "" {
		if !IsTaggedArtifactKind(r.Kind) {
			errs.Append("tag", fmt.Errorf("%w: kind %q does not support tag pinning", ErrInvalidRef, r.Kind))
		} else if err := validateTag(r.Tag); err != nil {
			errs.Append("tag", err)
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

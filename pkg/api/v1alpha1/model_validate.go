package v1alpha1

import (
	"fmt"
	"slices"
	"strings"
)

// KnownModelProviders is the set of provider families the validator
// recognizes. Keys are the canonical lowercase provider names. The value
// records whether the provider supports ambient runtime identity. Add a
// provider only after its runtime adapter and end-to-end coverage exist.
var KnownModelProviders = map[string]struct {
	AmbientIdentity bool
}{
	ModelProviderBedrock: {AmbientIdentity: true},
}

// Validate runs Model's structural checks.
//
// All model rules land at apply time on the Model (provider and auth live
// on one object):
//   - provider in the known set; model non-empty.
//   - auth.strategy in {runtime, secretRef, passthrough}; secretRef present
//     iff strategy is secretRef.
//   - "runtime" only for ambient-identity providers (currently bedrock);
//     key-based providers must declare secretRef or passthrough.
//
// Model is unversioned: identity is (namespace, name). Auth/endpoint edits
// are routine config mutations, not new versions.
func (m *Model) Validate() error {
	var errs FieldErrors
	errs = append(errs, ValidateObjectMeta(m.Metadata)...)
	errs = append(errs, validateModelSpec(&m.Spec)...)
	if len(errs) == 0 {
		return nil
	}
	return errs
}

func validateModelSpec(s *ModelSpec) FieldErrors {
	var errs FieldErrors

	provider := s.Provider
	providerInfo, providerKnown := KnownModelProviders[provider]
	if provider == "" {
		errs.Append("spec.provider", fmt.Errorf("%w", ErrRequiredField))
	} else if !providerKnown {
		errs.Append("spec.provider",
			fmt.Errorf("%w: %q (known: %v)", ErrInvalidFormat, s.Provider, knownModelProviderNames()))
	}

	if strings.TrimSpace(s.Model) == "" {
		errs.Append("spec.model", fmt.Errorf("%w", ErrRequiredField))
	}

	strategy := ""
	if s.Auth != nil {
		strategy = s.Auth.Strategy
		switch strategy {
		case ModelAuthStrategyRuntime, ModelAuthStrategyPassthrough:
			if s.Auth.SecretRef != nil {
				errs.Append("spec.auth.secretRef",
					fmt.Errorf("%w: secretRef is only valid with strategy %q", ErrInvalidFormat, ModelAuthStrategySecretRef))
			}
		case ModelAuthStrategySecretRef:
			if s.Auth.SecretRef == nil {
				errs.Append("spec.auth.secretRef", fmt.Errorf("%w: required for strategy %q", ErrRequiredField, ModelAuthStrategySecretRef))
			} else {
				errs = append(errs, validateSecretKeyRef(*s.Auth.SecretRef, "spec.auth.secretRef")...)
			}
		case "":
			errs.Append("spec.auth.strategy", fmt.Errorf("%w", ErrRequiredField))
		default:
			errs.Append("spec.auth.strategy",
				fmt.Errorf("%w: %q (expected %q, %q, or %q)", ErrInvalidFormat, s.Auth.Strategy,
					ModelAuthStrategyRuntime, ModelAuthStrategySecretRef, ModelAuthStrategyPassthrough))
		}
	}

	// Auth-strategy / provider compatibility. Omitted auth means the
	// provider default: "runtime" for ambient-identity providers;
	// key-based providers must declare a strategy.
	if providerKnown {
		if s.Auth == nil {
			if !providerInfo.AmbientIdentity {
				errs.Append("spec.auth",
					fmt.Errorf("%w: provider %q requires an explicit auth strategy (%q or %q)",
						ErrRequiredField, provider, ModelAuthStrategySecretRef, ModelAuthStrategyPassthrough))
			}
		} else if strategy == ModelAuthStrategyRuntime && !providerInfo.AmbientIdentity {
			errs.Append("spec.auth.strategy",
				fmt.Errorf("%w: strategy %q is only valid for ambient-identity providers (currently bedrock), not %q",
					ErrInvalidFormat, ModelAuthStrategyRuntime, provider))
		}
	}

	if s.Endpoint != nil && s.Endpoint.TLS != nil && s.Endpoint.TLS.CACertSecretRef != nil {
		errs = append(errs, validateSecretKeyRef(*s.Endpoint.TLS.CACertSecretRef, "spec.endpoint.tls.caCertSecretRef")...)
	}

	return errs
}

func validateSecretKeyRef(ref SecretKeyRef, path string) FieldErrors {
	var errs FieldErrors
	if err := validateNameField(ref.Name); err != nil {
		errs.Append(path+".name", err)
	}
	if ref.Namespace != "" && !namespaceRegex.MatchString(ref.Namespace) {
		errs.Append(path+".namespace", fmt.Errorf("%w: %q", ErrInvalidFormat, ref.Namespace))
	}
	return errs
}

func knownModelProviderNames() []string {
	out := make([]string, 0, len(KnownModelProviders))
	for k := range KnownModelProviders {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

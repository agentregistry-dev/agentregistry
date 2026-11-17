package v1alpha1

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

func validateMicrosoftRuntime(spec RuntimeSpec) FieldErrors {
	var errs FieldErrors
	switch spec.Type {
	case TypeMicrosoftFoundry:
		var cfg MicrosoftFoundryRuntimeConfig
		if err := decodeRuntimeConfig(spec.Config, &cfg); err != nil {
			errs.Append("spec.config", fmt.Errorf("%w: %v", ErrInvalidFormat, err))
			return errs
		}
		errs = append(errs, validateHTTPSURL(cfg.ProjectEndpoint, "spec.config.projectEndpoint")...)
		errs = append(errs, validateMicrosoftAuth(cfg.Auth)...)
	case TypeMicrosoftCopilotStudio:
		var cfg MicrosoftCopilotStudioRuntimeConfig
		if err := decodeRuntimeConfig(spec.Config, &cfg); err != nil {
			errs.Append("spec.config", fmt.Errorf("%w: %v", ErrInvalidFormat, err))
			return errs
		}
		if strings.TrimSpace(cfg.EnvironmentID) == "" {
			errs.Append("spec.config.environmentId", fmt.Errorf("%w", ErrRequiredField))
		}
		errs = append(errs, validateHTTPSURL(cfg.DataEndpoint, "spec.config.dataEndpoint")...)
		errs = append(errs, validateMicrosoftAuth(cfg.Auth)...)
	}
	return errs
}

func validateMicrosoftAuth(auth MicrosoftRuntimeAuth) FieldErrors {
	var errs FieldErrors
	if auth.OIDC == nil {
		errs.Append("spec.config.auth.oidc", fmt.Errorf("%w", ErrRequiredField))
		return errs
	}
	if strings.TrimSpace(auth.OIDC.Issuer) == "" {
		errs.Append("spec.config.auth.oidc.issuer", fmt.Errorf("%w", ErrRequiredField))
	} else {
		errs = append(errs, validateMicrosoftIssuer(auth.OIDC.Issuer)...)
	}
	if strings.TrimSpace(auth.OIDC.ClientID) == "" {
		errs.Append("spec.config.auth.oidc.clientId", fmt.Errorf("%w", ErrRequiredField))
	}
	if auth.OIDC.ClientSecretRef == nil {
		errs.Append("spec.config.auth.oidc.clientSecretRef", fmt.Errorf("%w", ErrRequiredField))
	} else {
		errs = append(errs, validateSecretKeyRef(*auth.OIDC.ClientSecretRef, "spec.config.auth.oidc.clientSecretRef")...)
		if strings.TrimSpace(auth.OIDC.ClientSecretRef.Key) == "" {
			errs.Append("spec.config.auth.oidc.clientSecretRef.key", fmt.Errorf("%w", ErrRequiredField))
		}
	}
	return errs
}

func validateMicrosoftIssuer(value string) FieldErrors {
	const path = "spec.config.auth.oidc.issuer"
	errs := validateHTTPSURL(value, path)
	if len(errs) != 0 {
		return errs
	}
	parsed, _ := url.Parse(value)
	if strings.Trim(parsed.Path, "/") == "" {
		errs.Append(path, fmt.Errorf("%w: must include a tenant path", ErrInvalidURL))
	}
	return errs
}

func validateHTTPSURL(value, path string) FieldErrors {
	var errs FieldErrors
	if strings.TrimSpace(value) == "" {
		errs.Append(path, fmt.Errorf("%w", ErrRequiredField))
		return errs
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		errs.Append(path, fmt.Errorf("%w: must be an https URL", ErrInvalidURL))
	}
	return errs
}

func decodeRuntimeConfig(config map[string]any, out any) error {
	raw, err := json.Marshal(config)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, out)
}

// Package kagent provides AgentRegistry deployment and discovery support for Kagent.
package kagent

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	corev1 "k8s.io/api/core/v1"
	k8svalidation "k8s.io/apimachinery/pkg/util/validation"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	RuntimeType             = "Kagent"
	defaultRuntimeNamespace = "kagent"
)

// Registers Kagent with the admission validator so Runtime{spec.type: Kagent} passes Validate.
func init() {
	v1alpha1.KnownRuntimeTypes[RuntimeType] = struct{}{}
	v1alpha1.RuntimeConfigValidators[RuntimeType] = validateRuntimeConfigForAdmission
}

func validateRuntimeConfigForAdmission(config map[string]any) error {
	if err := rejectRawToken(config); err != nil {
		return err
	}
	var fields struct {
		Auth authConfig `json:"auth,omitempty"`
	}
	if err := decodeJSONMap(config, &fields); err != nil {
		return fmt.Errorf("decode kagent runtime config: %w", err)
	}
	return validateAuthConfig(fields.Auth)
}

type runtimeConfig struct {
	URL              string                  `json:"kagentUrl"`
	Namespace        string                  `json:"namespace,omitempty"`
	Auth             authConfig              `json:"auth,omitempty"`
	ImagePullSecrets []string                `json:"imagePullSecrets,omitempty"`
	Deployment       runtimeDeploymentConfig `json:"deployment,omitempty"`
}

type runtimeConnectionConfig struct {
	URL       string     `json:"kagentUrl"`
	Namespace string     `json:"namespace,omitempty"`
	Auth      authConfig `json:"auth,omitempty"`
}

type runtimeDeploymentConfig struct {
	NodeSelector map[string]string   `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration `json:"tolerations,omitempty"`
	Affinity     *corev1.Affinity    `json:"affinity,omitempty"`
}

type authConfig struct {
	SecretRef *v1alpha1.SecretRef `json:"secretRef,omitempty"`
	UserID    string              `json:"userID,omitempty"`
}

type deployConfig struct {
	SecretRefs []string `json:"secretRefs,omitempty"`
}

func decodeJSONMap(m map[string]any, target any) error {
	if len(m) == 0 {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, target)
}

func decodeRuntimeConfig(m map[string]any) (runtimeConfig, error) {
	if err := rejectRawToken(m); err != nil {
		return runtimeConfig{}, err
	}
	var cfg runtimeConfig
	if err := decodeJSONMap(m, &cfg); err != nil {
		return runtimeConfig{}, fmt.Errorf("decode kagent runtime config: %w", err)
	}
	validatedURL, err := validateKagentURL(cfg.URL)
	if err != nil {
		return runtimeConfig{}, err
	}
	cfg.URL = validatedURL
	if cfg.Namespace != "" {
		if problems := k8svalidation.IsDNS1123Label(cfg.Namespace); len(problems) > 0 {
			return runtimeConfig{}, fmt.Errorf("kagent runtime config: namespace %q: %s", cfg.Namespace, strings.Join(problems, "; "))
		}
	}
	if err := validateAuthConfig(cfg.Auth); err != nil {
		return runtimeConfig{}, err
	}
	for i, name := range cfg.ImagePullSecrets {
		if problems := k8svalidation.IsDNS1123Subdomain(name); len(problems) > 0 {
			return runtimeConfig{}, fmt.Errorf(
				"kagent runtime config: imagePullSecrets[%d] %q is not a valid Kubernetes Secret name: %s",
				i,
				name,
				strings.Join(problems, "; "),
			)
		}
	}
	return cfg, nil
}

func decodeRuntimeConnectionConfig(m map[string]any) (runtimeConfig, error) {
	if err := rejectRawToken(m); err != nil {
		return runtimeConfig{}, err
	}
	var connection runtimeConnectionConfig
	if err := decodeJSONMap(m, &connection); err != nil {
		return runtimeConfig{}, fmt.Errorf("decode kagent runtime connection config: %w", err)
	}
	validatedURL, err := validateKagentURL(connection.URL)
	if err != nil {
		return runtimeConfig{}, err
	}
	connection.URL = validatedURL
	if connection.Namespace != "" {
		if problems := k8svalidation.IsDNS1123Label(connection.Namespace); len(problems) > 0 {
			return runtimeConfig{}, fmt.Errorf(
				"kagent runtime config: namespace %q: %s",
				connection.Namespace,
				strings.Join(problems, "; "),
			)
		}
	}
	if err := validateAuthConfig(connection.Auth); err != nil {
		return runtimeConfig{}, err
	}
	return runtimeConfig{
		URL:       connection.URL,
		Namespace: connection.Namespace,
		Auth:      connection.Auth,
	}, nil
}

func validateKagentURL(rawURL string) (string, error) {
	trimmedURL := strings.TrimSpace(rawURL)
	if trimmedURL == "" {
		return "", fmt.Errorf("kagent runtime config: kagentUrl is required")
	}
	parsedURL, err := url.Parse(trimmedURL)
	if err != nil {
		return "", fmt.Errorf("kagent runtime config: kagentUrl must be a valid URL: %w", err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("kagent runtime config: kagentUrl must use http or https")
	}
	if parsedURL.Hostname() == "" {
		return "", fmt.Errorf("kagent runtime config: kagentUrl must include a host")
	}
	if parsedURL.RawQuery != "" || parsedURL.ForceQuery {
		return "", fmt.Errorf("kagent runtime config: kagentUrl must not include a query string")
	}
	if parsedURL.Fragment != "" || strings.Contains(trimmedURL, "#") {
		return "", fmt.Errorf("kagent runtime config: kagentUrl must not include a fragment")
	}
	return trimmedURL, nil
}

func rejectRawToken(config map[string]any) error {
	for key, authValue := range config {
		if !strings.EqualFold(key, "auth") {
			continue
		}
		if err := rejectRawAuthToken(authValue); err != nil {
			return err
		}
	}
	return nil
}

func rejectRawAuthToken(authValue any) error {
	data, err := json.Marshal(authValue)
	if err != nil {
		return fmt.Errorf("encode kagent runtime auth config: %w", err)
	}
	var auth map[string]json.RawMessage
	if err := json.Unmarshal(data, &auth); err != nil {
		return fmt.Errorf("decode kagent runtime auth config: %w", err)
	}
	for key := range auth {
		if strings.EqualFold(key, "token") {
			return fmt.Errorf("kagent runtime config: auth.token is not supported; use auth.secretRef")
		}
	}
	return nil
}

func hasKeyFold(values map[string]any, want string) bool {
	for key := range values {
		if strings.EqualFold(key, want) {
			return true
		}
	}
	return false
}

func validateAuthConfig(auth authConfig) error {
	if auth.SecretRef == nil {
		return nil
	}
	if strings.TrimSpace(auth.SecretRef.Name) == "" {
		return fmt.Errorf("kagent runtime config: auth.secretRef.name is required")
	}
	if problems := k8svalidation.IsDNS1123Subdomain(auth.SecretRef.Name); len(problems) > 0 {
		return fmt.Errorf("kagent runtime config: auth.secretRef.name %q: %s", auth.SecretRef.Name, strings.Join(problems, "; "))
	}
	if strings.TrimSpace(auth.SecretRef.Key) == "" {
		return fmt.Errorf("kagent runtime config: auth.secretRef.key is required")
	}
	if auth.SecretRef.Namespace != "" {
		return fmt.Errorf("kagent runtime config: auth.secretRef.namespace is not supported; the Secret must be in the Runtime namespace")
	}
	return nil
}

func decodeDeployConfig(m map[string]any, targetKind string) (deployConfig, error) {
	if hasKeyFold(m, "secretRefs") && targetKind != v1alpha1.KindMCPServer {
		return deployConfig{}, fmt.Errorf("secretRefs is only supported for MCPServer deployments")
	}
	var cfg deployConfig
	if err := decodeJSONMap(m, &cfg); err != nil {
		return deployConfig{}, fmt.Errorf("decode kagent deploy config: %w", err)
	}
	for i, name := range cfg.SecretRefs {
		if problems := k8svalidation.IsDNS1123Subdomain(name); len(problems) > 0 {
			return deployConfig{}, fmt.Errorf("secretRefs[%d] %q is not a valid Kubernetes Secret name: %s", i, name, strings.Join(problems, "; "))
		}
	}
	return cfg, nil
}

package gitutil

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
	"github.com/agentregistry-dev/agentregistry/pkg/types"
)

// NewSecretCredentialResolver resolves repository credentials from the
// namespace-local Secret named by CredentialsRef.
func NewSecretCredentialResolver(resolver secret.Resolver) types.GitCredentialFunc {
	return func(ctx context.Context, namespace string, repo *v1alpha1.Repository) (*url.Userinfo, error) {
		if repo == nil || repo.CredentialsRef == nil || repo.CredentialsRef.Name == "" {
			return nil, nil
		}
		if resolver == nil {
			return nil, fmt.Errorf("secret resolver is not configured")
		}
		if namespace == "" {
			namespace = v1alpha1.DefaultNamespace
		}
		values, err := resolver.ResolveAll(ctx, v1alpha1.SecretRef{
			Namespace: namespace,
			Name:      repo.CredentialsRef.Name,
		})
		if err != nil {
			return nil, err
		}
		passwordValue, ok := values["password"]
		if !ok {
			return nil, fmt.Errorf(
				"resolve secret %s/%s: key %q: %w",
				namespace,
				repo.CredentialsRef.Name,
				"password",
				secret.ErrNotFound,
			)
		}

		username := strings.TrimSpace(string(values["username"].Reveal()))
		password := strings.TrimSpace(string(passwordValue.Reveal()))
		switch {
		case username != "" && password == "":
			return nil, fmt.Errorf("username set without password")
		case username == "" && strings.Contains(password, ":"):
			return nil, fmt.Errorf("password contains ':'; set username separately")
		case username != "":
			return url.UserPassword(username, password), nil
		case password != "":
			return url.User(password), nil
		default:
			return nil, nil
		}
	}
}

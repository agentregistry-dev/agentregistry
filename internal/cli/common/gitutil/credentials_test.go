package gitutil

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
	"github.com/agentregistry-dev/agentregistry/pkg/secret"
)

type fakeSecretResolver struct {
	values map[string]secret.SensitiveValue
	err    error
	ref    v1alpha1.SecretRef
}

func (*fakeSecretResolver) Resolve(context.Context, v1alpha1.SecretRef) (secret.SensitiveValue, error) {
	return secret.SensitiveValue{}, errors.New("unexpected Resolve call")
}

func (r *fakeSecretResolver) ResolveAll(_ context.Context, ref v1alpha1.SecretRef) (map[string]secret.SensitiveValue, error) {
	r.ref = ref
	return r.values, r.err
}

func TestSecretCredentialResolver(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		repo      *v1alpha1.Repository
		resolver  *fakeSecretResolver
		want      *url.Userinfo
		wantRef   v1alpha1.SecretRef
		wantErr   string
	}{
		{
			name:     "no credentials ref is anonymous",
			repo:     &v1alpha1.Repository{},
			resolver: &fakeSecretResolver{},
		},
		{
			name: "password uses token-only authentication",
			repo: repositoryWithCredentials("github"),
			resolver: &fakeSecretResolver{values: map[string]secret.SensitiveValue{
				"password": secret.NewSensitiveValue([]byte("ghp-secret")),
			}},
			want:    url.User("ghp-secret"),
			wantRef: v1alpha1.SecretRef{Namespace: v1alpha1.DefaultNamespace, Name: "github"},
		},
		{
			name:      "username and password use basic authentication",
			namespace: "team-a",
			repo:      repositoryWithCredentials("gitlab"),
			resolver: &fakeSecretResolver{values: map[string]secret.SensitiveValue{
				"username": secret.NewSensitiveValue([]byte("oauth2")),
				"password": secret.NewSensitiveValue([]byte("glpat-secret")),
			}},
			want:    url.UserPassword("oauth2", "glpat-secret"),
			wantRef: v1alpha1.SecretRef{Namespace: "team-a", Name: "gitlab"},
		},
		{
			name:     "missing password is an error",
			repo:     repositoryWithCredentials("github"),
			resolver: &fakeSecretResolver{values: map[string]secret.SensitiveValue{}},
			wantRef:  v1alpha1.SecretRef{Namespace: v1alpha1.DefaultNamespace, Name: "github"},
			wantErr:  `key "password"`,
		},
		{
			name: "username without password is an error",
			repo: repositoryWithCredentials("github"),
			resolver: &fakeSecretResolver{values: map[string]secret.SensitiveValue{
				"username": secret.NewSensitiveValue([]byte("git")),
				"password": secret.NewSensitiveValue(nil),
			}},
			wantRef: v1alpha1.SecretRef{Namespace: v1alpha1.DefaultNamespace, Name: "github"},
			wantErr: "username set without password",
		},
		{
			name: "token-only password containing colon is an error",
			repo: repositoryWithCredentials("github"),
			resolver: &fakeSecretResolver{values: map[string]secret.SensitiveValue{
				"password": secret.NewSensitiveValue([]byte("git:ghp-secret")),
			}},
			wantRef: v1alpha1.SecretRef{Namespace: v1alpha1.DefaultNamespace, Name: "github"},
			wantErr: "password contains ':'",
		},
		{
			name:     "secret resolution failure is returned",
			repo:     repositoryWithCredentials("github"),
			resolver: &fakeSecretResolver{err: secret.ErrNotFound},
			wantRef:  v1alpha1.SecretRef{Namespace: v1alpha1.DefaultNamespace, Name: "github"},
			wantErr:  secret.ErrNotFound.Error(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NewSecretCredentialResolver(tt.resolver)(t.Context(), tt.namespace, tt.repo)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("error = %v, want it to contain %q", err, tt.wantErr)
				}
			} else if err != nil {
				t.Fatalf("resolve credentials: %v", err)
			}
			if userinfoString(got) != userinfoString(tt.want) {
				t.Fatalf("credentials = %q, want %q", userinfoString(got), userinfoString(tt.want))
			}
			if tt.resolver.ref != tt.wantRef {
				t.Fatalf("secret ref = %#v, want %#v", tt.resolver.ref, tt.wantRef)
			}
		})
	}
}

func TestSourceUsesSecretCredentials(t *testing.T) {
	installFakeGit(t, `if [ "$2" != "https://x-access-token:ghp-secret@github.com/org/private.git" ]; then
  printf 'unexpected URL: %s\n' "$2" >&2
  exit 1
fi
printf '0123456789abcdef0123456789abcdef01234567\trefs/heads/main\n'
`)
	resolver := &fakeSecretResolver{values: map[string]secret.SensitiveValue{
		"username": secret.NewSensitiveValue([]byte("x-access-token")),
		"password": secret.NewSensitiveValue([]byte("ghp-secret")),
	}}
	source := NewSource(NewSecretCredentialResolver(resolver))

	got, err := source.Pin(t.Context(), "team-a", &v1alpha1.Repository{
		URL:            "https://github.com/org/private.git",
		Branch:         "main",
		CredentialsRef: &v1alpha1.LocalSecretReference{Name: "github"},
	})
	if err != nil {
		t.Fatalf("pin private repository: %v", err)
	}
	if got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("commit = %q, want fake Git commit", got)
	}
	if resolver.ref != (v1alpha1.SecretRef{Namespace: "team-a", Name: "github"}) {
		t.Fatalf("secret ref = %#v, want namespace-local reference", resolver.ref)
	}
}

func repositoryWithCredentials(name string) *v1alpha1.Repository {
	return &v1alpha1.Repository{
		CredentialsRef: &v1alpha1.LocalSecretReference{Name: name},
	}
}

func userinfoString(value *url.Userinfo) string {
	if value == nil {
		return ""
	}
	return value.String()
}

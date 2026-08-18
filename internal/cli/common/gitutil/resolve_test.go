package gitutil

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestIsFullCommitSHA(t *testing.T) {
	good := strings.Repeat("a", 40)
	if !isFullCommitSHA(good) {
		t.Fatalf("expected %q to be a full SHA", good)
	}
	for _, bad := range []string{"", "main", strings.Repeat("a", 39), strings.Repeat("a", 41), "z" + strings.Repeat("a", 39)} {
		if isFullCommitSHA(bad) {
			t.Fatalf("expected %q NOT to be a full SHA", bad)
		}
	}
}

func TestFirstLSRemoteSHA(t *testing.T) {
	tests := []struct {
		name string
		in   string
		ref  string
		want string
	}{
		{"branch", "deadbeef\trefs/heads/main\n", "main", "deadbeef"},
		{"empty", "", "main", ""},
		{"blank lines", "\n  \n", "main", ""},
		{
			name: "annotated tag prefers dereferenced commit",
			in:   "1111111\trefs/tags/v1\n2222222\trefs/tags/v1^{}\n",
			ref:  "v1",
			want: "2222222",
		},
		{"first of many", "aaa\trefs/heads/a\nbbb\trefs/heads/b\n", "a", "aaa"},
		{
			// Ambiguous name that is both a branch and a tag: resolve
			// deterministically, following git's ref precedence (tag wins).
			name: "tag preferred over branch for same name (git precedence)",
			in:   "ttttttt\trefs/tags/release\nhhhhhhh\trefs/heads/release\n",
			ref:  "release",
			want: "ttttttt",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLSRemoteSHA(tt.in, tt.ref); got != tt.want {
				t.Fatalf("firstLSRemoteSHA(%q, %q) = %q, want %q", tt.in, tt.ref, got, tt.want)
			}
		})
	}
}

func TestSafeGitRef(t *testing.T) {
	for _, ok := range []string{"", "main", "feature/x", "v1.2.3", "abc123", "release/2024-01"} {
		if err := safeGitRef(ok); err != nil {
			t.Fatalf("safeGitRef(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"-x", "--upload-pack=touch /tmp/x", "--exec=evil"} {
		if err := safeGitRef(bad); err == nil {
			t.Fatalf("safeGitRef(%q) should reject option-like ref", bad)
		}
	}
}

func TestResolveRefRejectsOptionInjection(t *testing.T) {
	// A ref that git would parse as an option must be rejected before exec.
	if _, err := ResolveRefContext(context.Background(), "https://github.com/org/repo", "--upload-pack=touch /tmp/pwn", nil); err == nil {
		t.Fatal("expected ResolveRefContext to reject an option-like ref")
	}
}

func TestResolveRefPassesThroughFullSHA(t *testing.T) {
	// A full SHA needs no network round-trip; it is returned lowercased.
	sha := strings.Repeat("A", 40)
	got, err := ResolveRefContext(context.Background(), "https://github.com/org/repo", sha, nil)
	if err != nil {
		t.Fatalf("ResolveRefContext: %v", err)
	}
	if got != strings.ToLower(sha) {
		t.Fatalf("ResolveRefContext passthrough = %q, want lowercased SHA", got)
	}
}

func TestAuthenticate(t *testing.T) {
	const cloneURL = "https://github.com/org/repo.git"

	execURL, safeURL, err := authenticate(cloneURL, nil)
	if err != nil {
		t.Fatalf("authenticate(nil): %v", err)
	}
	if execURL != cloneURL || safeURL != cloneURL {
		t.Fatalf("authenticate(nil) = (%q, %q), want the URL unchanged", execURL, safeURL)
	}

	execURL, safeURL, err = authenticate(cloneURL, url.UserPassword("git", "ghp_secret"))
	if err != nil {
		t.Fatalf("authenticate: %v", err)
	}
	if execURL != "https://git:ghp_secret@github.com/org/repo.git" {
		t.Fatalf("exec URL = %q, want the token spliced in", execURL)
	}
	if strings.Contains(safeURL, "ghp_secret") {
		t.Fatalf("safe URL = %q, must not carry the token", safeURL)
	}
}

func TestResolveRefRedactsCredentialsInErrors(t *testing.T) {
	// Resolve errors are persisted into resource status, so a spliced token must
	// never reach the error string. A cancelled ctx fails ls-remote without
	// touching the network.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveRefContext(ctx, "https://github.com/org/repo.git", "main", url.UserPassword("git", "ghp_secret"))
	if err == nil {
		t.Fatal("expected ls-remote to fail under a cancelled context")
	}
	if strings.Contains(err.Error(), "ghp_secret") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestPinRefCredentialFailureIsWrapped(t *testing.T) {
	repo := &v1alpha1.Repository{URL: "https://github.com/org/repo", Branch: "main"}
	want := errors.New("boom")
	_, err := PinRef(context.Background(), "ns", repo, func(context.Context, string, *v1alpha1.Repository) (*url.Userinfo, error) {
		return nil, want
	})
	if !errors.Is(err, want) {
		t.Fatalf("PinRef error = %v, want it to wrap %v", err, want)
	}
	if !strings.Contains(err.Error(), "resolve git credentials") {
		t.Fatalf("PinRef error = %v, want it to name credential resolution", err)
	}
}

func TestPinRefRequiresURL(t *testing.T) {
	if _, err := PinRef(context.Background(), "ns", &v1alpha1.Repository{}, nil); err == nil {
		t.Fatal("expected PinRef to reject a repository with no url")
	}
	if _, err := PinAndCopy(context.Background(), "ns", nil, t.TempDir(), nil); err == nil {
		t.Fatal("expected PinAndCopy to reject a nil repository")
	}
}

// A pinned full SHA needs no network, so PinRef must short-circuit to it.
func TestPinRefPrefersExplicitCommit(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	got, err := PinRef(context.Background(), "ns", &v1alpha1.Repository{
		URL: "https://github.com/org/repo", Branch: "main", Commit: sha,
	}, nil)
	if err != nil {
		t.Fatalf("PinRef: %v", err)
	}
	if got != sha {
		t.Fatalf("PinRef = %q, want %q", got, sha)
	}
}

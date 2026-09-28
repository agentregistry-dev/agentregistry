package gitutil

import (
	"context"
	"errors"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-git/go-git/v5/plumbing"

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
	sha := func(c string) string { return strings.Repeat(c, 40) }
	ref := func(name, hash string) *plumbing.Reference {
		return plumbing.NewReferenceFromStrings(name, hash)
	}
	tests := []struct {
		name string
		in   []*plumbing.Reference
		ref  string
		want string
	}{
		{"branch", []*plumbing.Reference{ref("refs/heads/main", sha("d"))}, "main", sha("d")},
		{"empty", nil, "main", ""},
		{
			name: "annotated tag prefers dereferenced commit",
			in:   []*plumbing.Reference{ref("refs/tags/v1", sha("1")), ref("refs/tags/v1^{}", sha("2"))},
			ref:  "v1",
			want: sha("2"),
		},
		{"first of many", []*plumbing.Reference{ref("refs/heads/a", sha("a")), ref("refs/heads/b", sha("b"))}, "a", sha("a")},
		{
			// Ambiguous name that is both a branch and a tag: resolve
			// deterministically, following git's ref precedence (tag wins).
			name: "tag preferred over branch for same name (git precedence)",
			in:   []*plumbing.Reference{ref("refs/heads/release", sha("b")), ref("refs/tags/release", sha("c"))},
			ref:  "release",
			want: sha("c"),
		},
		{
			name: "symbolic HEAD resolves to its target",
			in:   []*plumbing.Reference{ref("HEAD", "ref: refs/heads/main"), ref("refs/heads/main", sha("e"))},
			ref:  "HEAD",
			want: sha("e"),
		},
		{"ignores refs that only share a prefix", []*plumbing.Reference{ref("refs/heads/mainline", sha("f"))}, "main", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLSRemoteSHA(tt.in, tt.ref); got != tt.want {
				t.Fatalf("firstLSRemoteSHA(%v, %q) = %q, want %q", tt.in, tt.ref, got, tt.want)
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

func TestResolveRefRedactsCredentialsInErrors(t *testing.T) {
	// Resolve errors are persisted into resource status, so a spliced token must
	// never reach the error string. A cancelled ctx fails the ref listing
	// without touching the network.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ResolveRefContext(ctx, "https://github.com/org/repo.git", "main", url.UserPassword("git", "ghp_secret"))
	if err == nil {
		t.Fatal("expected the ref listing to fail under a cancelled context")
	}
	if strings.Contains(err.Error(), "ghp_secret") {
		t.Fatalf("error leaks the token: %v", err)
	}
}

func TestGitErrorsIncludeRedactedServerMessage(t *testing.T) {
	// Git servers may echo credentials back; errors are persisted to status.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication failed for x-access-token:ghp_secret", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	repoURL := srv.URL + "/org/private.git"
	auth := url.UserPassword("x-access-token", "ghp_secret")

	_, resolveErr := ResolveRefContext(t.Context(), repoURL, "main", auth)
	cloneErr := CloneAndCopyContext(t.Context(), repoURL, "main", "", "", t.TempDir(), false, auth)
	for _, err := range []error{resolveErr, cloneErr} {
		if err == nil {
			t.Fatal("expected git to fail")
		}
		if !strings.Contains(err.Error(), "authentication failed") {
			t.Fatalf("error = %v, want server message", err)
		}
		if strings.Contains(err.Error(), "ghp_secret") || strings.Contains(err.Error(), "x-access-token") {
			t.Fatalf("error leaks credentials: %v", err)
		}
	}
}

func TestSanitizeGitDiagnosticBoundsOutputAndRedactsTokenOnlyAuth(t *testing.T) {
	auth := url.User("ghp_secret")
	diagnostic := strings.Repeat("x", maxGitDiagnosticRunes+100) + " ghp_secret"
	got := sanitizeGitDiagnostic(diagnostic, auth)
	if strings.Contains(got, "ghp_secret") {
		t.Fatalf("diagnostic leaks token-only auth: %q", got)
	}
	if len([]rune(got)) > maxGitDiagnosticRunes+3 {
		t.Fatalf("diagnostic length = %d, want at most %d", len([]rune(got)), maxGitDiagnosticRunes+3)
	}
	if !strings.HasPrefix(got, "...") {
		t.Fatalf("diagnostic = %q, want truncation marker", got)
	}
}

func TestCloneAndCopyFetchesPinnedCommitWithAuth(t *testing.T) {
	repoURL, dir := serveGitRepo(t, "x-access-token", "ghp_secret")
	auth := url.UserPassword("x-access-token", "ghp_secret")
	writeAndCommit(t, dir, "one")
	first := runGit(t, dir, "rev-parse", "HEAD")
	writeAndCommit(t, dir, "two")
	runGit(t, dir, "tag", "-a", "v1", "-m", "v1", first)

	tests := []struct {
		name, branch, commit, want string
	}{
		{"default HEAD", "", "", "two"},
		{"branch", "main", "", "two"},
		{"annotated tag", "v1", "", "one"},
		{"non-tip commit", "main", first, "one"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := t.TempDir()
			if err := CloneAndCopyContext(t.Context(), repoURL, tt.branch, tt.commit, "sub", target, false, auth); err != nil {
				t.Fatalf("CloneAndCopyContext: %v", err)
			}
			got, err := os.ReadFile(filepath.Join(target, "file.txt"))
			if err != nil {
				t.Fatalf("read copied file: %v", err)
			}
			if string(got) != tt.want {
				t.Fatalf("file.txt = %q, want %q", got, tt.want)
			}
		})
	}

	if _, err := ResolveRefContext(t.Context(), repoURL, "main", url.UserPassword("x-access-token", "wrong")); err == nil {
		t.Fatal("expected wrong credentials to be rejected")
	}
	if _, err := ResolveRefContext(t.Context(), repoURL, "missing", auth); !errors.Is(err, ErrRefNotFound) {
		t.Fatalf("ResolveRefContext(missing) error = %v, want ErrRefNotFound", err)
	}
}

// serveGitRepo serves a fixture repository over smart HTTP (git http-backend),
// rejecting requests whose Basic auth is not username/password.
func serveGitRepo(t *testing.T, username, password string) (repoURL, dir string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required to serve fixture repositories")
	}
	root := t.TempDir()
	dir = filepath.Join(root, "org", "repo.git")
	runGit(t, root, "init", "--initial-branch=main", dir)
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	// go-git only fetches by hash from servers advertising this capability.
	runGit(t, dir, "config", "uploadpack.allowReachableSHA1InWant", "true")
	backend := &cgi.Handler{
		Path: gitPath,
		Args: []string{"http-backend"},
		Env:  []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != username || pass != password {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		backend.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/org/repo.git", dir
}

// writeAndCommit commits content as sub/file.txt.
func writeAndCommit(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "sub", "file.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", content)
}

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestSourceCredentialFailureIsWrapped(t *testing.T) {
	repo := &v1alpha1.Repository{URL: "https://github.com/org/repo", Branch: "main"}
	want := errors.New("boom")
	src := NewSource(func(context.Context, string, *v1alpha1.Repository) (*url.Userinfo, error) {
		return nil, want
	})
	_, err := src.Pin(context.Background(), "ns", repo)
	if !errors.Is(err, want) {
		t.Fatalf("Pin error = %v, want it to wrap %v", err, want)
	}
	if !strings.Contains(err.Error(), "resolve git credentials") {
		t.Fatalf("Pin error = %v, want it to name credential resolution", err)
	}
}

func TestSourceRequiresURL(t *testing.T) {
	src := NewSource(nil)
	if _, err := src.Pin(context.Background(), "ns", &v1alpha1.Repository{}); err == nil {
		t.Fatal("expected Pin to reject a repository with no url")
	}
	if _, err := src.Fetch(context.Background(), "ns", nil, t.TempDir()); err == nil {
		t.Fatal("expected Fetch to reject a nil repository")
	}
}

// A pinned full SHA needs no network, so Pin must short-circuit to it.
func TestSourcePinPrefersExplicitCommit(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	got, err := NewSource(nil).Pin(context.Background(), "ns", &v1alpha1.Repository{
		URL: "https://github.com/org/repo", Branch: "main", Commit: sha,
	})
	if err != nil {
		t.Fatalf("Pin: %v", err)
	}
	if got != sha {
		t.Fatalf("Pin = %q, want %q", got, sha)
	}
}

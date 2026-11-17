package gitutil

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"net/http"
	"net/http/cgi"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const fixturePassword = "ghp-secret"

var fixtureAuth = url.UserPassword("x-access-token", fixturePassword)

// serveFixture serves a repo over git http-backend whose tip adds hostile entries.
func serveFixture(t *testing.T, populate, allowSHAWants bool) (repoURL, first, tip string) {
	t.Helper()
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is required to serve fixture repositories")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "org", "repo.git")
	git := func(args ...string) string {
		out, err := exec.Command(gitPath, append([]string{"-C", dir}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	git("init", "--initial-branch=main")
	git("config", "user.email", "test@example.com")
	git("config", "user.name", "Test User")
	// go-git only fetches by hash from servers advertising this capability.
	git("config", "uploadpack.allowReachableSHA1InWant", strconv.FormatBool(allowSHAWants))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if user, pass, _ := r.BasicAuth(); user != fixtureAuth.Username() || pass != fixturePassword {
			http.Error(w, "authentication failed", http.StatusUnauthorized)
			return
		}
		(&cgi.Handler{Path: gitPath, Args: []string{"http-backend"}, Env: []string{"GIT_PROJECT_ROOT=" + root, "GIT_HTTP_EXPORT_ALL=1"}}).ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	repoURL = srv.URL + "/org/repo.git"
	if !populate {
		return repoURL, "", ""
	}
	commit := func(files map[string]string) string {
		for rel, content := range files {
			path := filepath.Join(dir, rel)
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatal(err)
			}
			var err error
			if target, ok := strings.CutPrefix(content, "-> "); ok {
				err = os.Symlink(target, path)
			} else {
				err = os.WriteFile(path, []byte(content), 0o644)
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		git("add", ".")
		git("update-index", "--chmod=+x", "--", "skill/run.sh")
		if first != "" {
			git("update-index", "--add", "--cacheinfo", "160000,"+first+",mods/mod")
		}
		git("commit", "-m", "fixture")
		return git("rev-parse", "HEAD")
	}
	first = commit(map[string]string{
		"README.md": "root", "skill/SKILL.md": "skill", "skill/run.sh": "run",
		"skill/nested/deep.txt": "deep", "skill/link.md": "-> SKILL.md",
	})
	tip = commit(map[string]string{"up/link": "-> ../skill/SKILL.md", "abs/link": "-> /etc/passwd", "dirlink": "-> skill"})
	git("tag", "light", first)
	git("tag", "-a", "v1", "-m", "v1", first)
	git("branch", "release", tip)
	git("tag", "release", first)
	return repoURL, first, tip
}

func TestResolveRef(t *testing.T) {
	repoURL, first, tip := serveFixture(t, true, true)
	emptyURL, _, _ := serveFixture(t, false, true)
	wrong := url.UserPassword("x-access-token", "wrong")
	for _, tt := range []struct {
		url, ref, want, wantErr string
		auth                    *url.Userinfo
	}{
		{ref: "", want: tip},
		{ref: "main", want: tip},
		{ref: "light", want: first},
		{ref: "v1", want: first},      // annotated tag peels to its commit
		{ref: "release", want: first}, // tag beats branch
		{ref: "refs/heads/release", want: tip},
		{ref: strings.ToUpper(first), want: first, auth: wrong}, // no network
		{ref: "missing", wantErr: ErrRefNotFound.Error()},
		{ref: "ain", wantErr: ErrRefNotFound.Error()},
		{ref: "heads/main", wantErr: ErrRefNotFound.Error()},
		{ref: "main", auth: wrong, wantErr: "authentication"},
		{url: emptyURL, wantErr: ErrRefNotFound.Error()},
	} {
		got, err := resolveRef(t.Context(), cmp.Or(tt.url, repoURL), tt.ref, cmp.Or(tt.auth, fixtureAuth))
		if got != tt.want || (err == nil) != (tt.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("resolveRef(%q) = %q, %v; want %q, %q", tt.ref, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestFetch(t *testing.T) {
	// Servers without SHA wants take the fetch-all-refs fallback.
	for _, allowSHAWants := range []bool{true, false} {
		t.Run(fmt.Sprintf("allowSHAWants=%t", allowSHAWants), func(t *testing.T) { testFetch(t, allowSHAWants) })
	}
}

func testFetch(t *testing.T, allowSHAWants bool) {
	repoURL, first, tip := serveFixture(t, true, allowSHAWants)
	skill := map[string]string{"SKILL.md": "644 skill", "run.sh": "755 run", "nested/deep.txt": "644 deep", "link.md": "-> SKILL.md"}
	root := map[string]string{"README.md": "644 root"}
	for rel, v := range skill {
		root["skill/"+rel] = v
	}
	creds := func(context.Context, string, *v1alpha1.Repository) (*url.Userinfo, error) { return fixtureAuth, nil }
	for _, tt := range []struct {
		commit, sub, wantErr string
		limits               Limits
		want                 map[string]string // rel path -> "<mode> <content>" or "-> <target>"
	}{
		{commit: first, want: root},
		{sub: "skill/", want: skill},
		{sub: "skill/SKILL.md", want: map[string]string{"SKILL.md": "644 skill"}},
		{sub: "mods", want: map[string]string{}}, // submodules are skipped
		{sub: "up", wantErr: "escapes the source directory"},
		{sub: "abs", wantErr: "escapes the source directory"},
		{sub: "dirlink", wantErr: "not a directory or regular file"},
		{sub: "nope", wantErr: `subdirectory "nope" not found in repository`},
		{sub: "../x", wantErr: "escapes repository"},
		{sub: "/etc", wantErr: "escapes repository"},
		{sub: "skill", limits: Limits{MaxBytes: 5, MaxEntries: 100}, wantErr: "exceeds the size limit"},
		{sub: "skill", limits: Limits{MaxBytes: 1 << 20, MaxEntries: 2}, wantErr: "exceeds 2 entries"},
	} {
		dest := filepath.Join(t.TempDir(), "out")
		limits := cmp.Or(tt.limits, Limits{MaxBytes: 1 << 20, MaxEntries: 100})
		sha, err := NewSource(creds, limits).Fetch(t.Context(), "ns", &v1alpha1.Repository{URL: repoURL, Commit: tt.commit, Subfolder: tt.sub}, dest)
		if tt.wantErr != "" {
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("Fetch(%q) error = %v, want %q", tt.sub, err, tt.wantErr)
			}
			continue
		}
		if got := snapshot(t, dest); err != nil || sha != cmp.Or(tt.commit, tip) || !maps.Equal(got, tt.want) {
			t.Errorf("Fetch(%q) = %q, %v, %v; want %s, %v", tt.sub, sha, err, got, cmp.Or(tt.commit, tip), tt.want)
		}
	}
}

// snapshot maps each file and symlink under dir to its mode and content or target.
func snapshot(t *testing.T, dir string) map[string]string {
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		if target, err := os.Readlink(path); err == nil {
			got[rel] = "-> " + target
			return nil
		}
		info, _ := d.Info()
		content, err := os.ReadFile(path)
		got[rel] = fmt.Sprintf("%o %s", info.Mode().Perm(), content)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestErrorsAreRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "authentication failed for https://x-access-token:ghp-secret@host", http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	creds := func(context.Context, string, *v1alpha1.Repository) (*url.Userinfo, error) { return fixtureAuth, nil }
	_, resolveErr := resolveRef(t.Context(), srv.URL+"/org/private.git", "main", fixtureAuth)
	_, fetchErr := NewSource(creds, Limits{}).Fetch(t.Context(), "ns", &v1alpha1.Repository{
		URL: srv.URL + "/org/private.git", Commit: strings.Repeat("a", 40),
	}, t.TempDir())
	for _, err := range []error{resolveErr, fetchErr} {
		if err == nil || !strings.Contains(err.Error(), "authentication failed for https://xxxxx:xxxxx@host") {
			t.Fatalf("error = %v, want redacted server message", err)
		}
	}
	got := redact(errors.New(strings.Repeat("x", maxGitDiagnosticRunes+100)+" ghp_token"), url.User("ghp_token")).Error()
	if strings.Contains(got, "ghp_token") || len([]rune(got)) != maxGitDiagnosticRunes+3 || !strings.HasPrefix(got, "...") {
		t.Fatalf("redact = %q, want token masked and output bounded", got)
	}
	// A username with a password is not secret, so only the password is masked.
	if got := redact(errors.New("fetch http://git-fixture/acme/private.git: pw"), url.UserPassword("git", "pw")).Error(); got != "fetch http://git-fixture/acme/private.git: xxxxx" {
		t.Fatalf("redact = %q, want only the password masked", got)
	}
}

func TestPin(t *testing.T) {
	const sha = "0123456789abcdef0123456789abcdef01234567"
	boom := errors.New("boom")
	failing := func(context.Context, string, *v1alpha1.Repository) (*url.Userinfo, error) { return nil, boom }
	for _, tt := range []struct {
		name, want, wantErr string
		repo                *v1alpha1.Repository
		source              Source
	}{
		{name: "commit beats branch without network", repo: &v1alpha1.Repository{URL: "https://github.com/org/repo", Branch: "main", Commit: sha}, want: sha},
		{name: "url required", repo: &v1alpha1.Repository{}, wantErr: "repository url is required"},
		{name: "credential failure wrapped", repo: &v1alpha1.Repository{URL: "https://github.com/org/repo"}, source: NewSource(failing, Limits{}), wantErr: "resolve git credentials: boom"},
	} {
		got, err := cmp.Or(tt.source, NewSource(nil, Limits{})).Pin(t.Context(), "ns", tt.repo)
		if got != tt.want || (err == nil) != (tt.wantErr == "") || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%s: Pin = %q, %v; want %q, %q", tt.name, got, err, tt.want, tt.wantErr)
		}
		if tt.source != nil && !errors.Is(err, boom) {
			t.Errorf("%s: error %v does not wrap %v", tt.name, err, boom)
		}
	}
	if _, err := NewSource(nil, Limits{}).Fetch(t.Context(), "ns", nil, t.TempDir()); err == nil {
		t.Fatal("expected Fetch to reject a nil repository")
	}
}

package gitutil

import (
	"strings"
	"testing"
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
		want string
	}{
		{"branch", "deadbeef\trefs/heads/main\n", "deadbeef"},
		{"empty", "", ""},
		{"blank lines", "\n  \n", ""},
		{
			name: "annotated tag prefers dereferenced commit",
			in:   "1111111\trefs/tags/v1\n2222222\trefs/tags/v1^{}\n",
			want: "2222222",
		},
		{"first of many", "aaa\trefs/heads/a\nbbb\trefs/heads/b\n", "aaa"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := firstLSRemoteSHA(tt.in); got != tt.want {
				t.Fatalf("firstLSRemoteSHA(%q) = %q, want %q", tt.in, got, tt.want)
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
	if _, err := ResolveRef("https://github.com/org/repo", "--upload-pack=touch /tmp/pwn"); err == nil {
		t.Fatal("expected ResolveRef to reject an option-like ref")
	}
}

func TestResolveRefPassesThroughFullSHA(t *testing.T) {
	// A full SHA needs no network round-trip; it is returned lowercased.
	sha := strings.Repeat("A", 40)
	got, err := ResolveRef("https://github.com/org/repo", sha)
	if err != nil {
		t.Fatalf("ResolveRef: %v", err)
	}
	if got != strings.ToLower(sha) {
		t.Fatalf("ResolveRef passthrough = %q, want lowercased SHA", got)
	}
}

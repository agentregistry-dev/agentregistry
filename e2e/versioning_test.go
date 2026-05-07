//go:build e2e

// Tests for the tagged-resource contract:
//   - omitted metadata.tag applies to the literal "latest" tag
//   - re-applying unchanged content is a no-op
//   - applying changed content to the same tag replaces that row
//   - explicit tags remain separate rows
//   - manifests with metadata.version set are rejected at decode time
//   - delete defaults to the "latest" tag; --all-versions clears every tag
//
// Lives next to declarative_test.go and reuses its writeDeclarativeYAML and
// resourceURL helpers.

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// applyAgentWithDesc applies an Agent YAML for `name` whose spec contains a
// single description field set to `desc`. With an empty tag, apply targets the
// literal "latest" tag.
func applyAgentWithDesc(t *testing.T, regURL, tmpDir, name, desc string) {
	t.Helper()
	applyAgentWithTagAndDesc(t, regURL, tmpDir, name, "", desc)
}

func applyAgentWithTagAndDesc(t *testing.T, regURL, tmpDir, name, tag, desc string) {
	t.Helper()
	tagLine := ""
	if tag != "" {
		tagLine = fmt.Sprintf("  tag: %s\n", tag)
	}
	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
%s
spec:
  source:
    image: ghcr.io/e2e-test/versioning-agent:latest
  description: %q
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, name, tagLine, desc)
	path := writeDeclarativeYAML(t, tmpDir, fmt.Sprintf("agent-%d.yaml", time.Now().UnixNano()), yaml)
	result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Agent/"+name)
}

// agentTag runs `arctl get agent <name> -o json --registry-url ...` and returns
// metadata.tag. Fails the test if the call fails or the value is missing.
func agentTag(t *testing.T, regURL, tmpDir, name string, args ...string) string {
	t.Helper()
	cmdArgs := append([]string{"get", "agent", name}, args...)
	cmdArgs = append(cmdArgs, "-o", "json", "--registry-url", regURL)
	result := RunArctl(t, tmpDir, cmdArgs...)
	RequireSuccess(t, result)
	var decoded struct {
		Metadata struct {
			Tag string `json:"tag"`
		} `json:"metadata"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &decoded); err != nil {
		t.Fatalf("decode get -o json: %v\nstdout: %s", err, result.Stdout)
	}
	return decoded.Metadata.Tag
}

// agentTagCount runs `arctl get agent <name> --all-versions -o json` and
// returns the number of tag rows returned. --all-versions is kept here as the
// backward-compatible alias for --all-tags.
func agentTagCount(t *testing.T, regURL, tmpDir, name string) int {
	t.Helper()
	result := RunArctl(t, tmpDir, "get", "agent", name,
		"--all-versions", "-o", "json", "--registry-url", regURL)
	RequireSuccess(t, result)
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(result.Stdout), &arr); err != nil {
		t.Fatalf("decode get --all-tags -o json: %v\nstdout: %s", err, result.Stdout)
	}
	return len(arr)
}

// TestVersioning_ApplyAndIdempotency verifies the core tagged-resource
// contract on the apply path:
//   - first blank-tag apply lands as the literal latest tag
//   - re-applying the identical spec is a no-op
//   - applying changed content replaces the same latest row
func TestVersioning_ApplyAndIdempotency(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := UniqueAgentName("verapply")

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})

	// Step 1: first blank-tag apply -> latest.
	applyAgentWithDesc(t, regURL, tmpDir, name, "first")
	if got := agentTag(t, regURL, tmpDir, name); got != "latest" {
		t.Fatalf("after first apply: expected metadata.tag=latest, got %q", got)
	}
	if got := agentTagCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after first apply: expected 1 tag row, got %d", got)
	}

	// Step 2: same spec → idempotent (no new row).
	applyAgentWithDesc(t, regURL, tmpDir, name, "first")
	if got := agentTag(t, regURL, tmpDir, name); got != "latest" {
		t.Fatalf("after re-apply: expected metadata.tag=latest, got %q", got)
	}
	if got := agentTagCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after re-apply: expected 1 tag row, got %d", got)
	}

	// Step 3: changed content -> same latest row is replaced.
	applyAgentWithDesc(t, regURL, tmpDir, name, "second")
	if got := agentTag(t, regURL, tmpDir, name); got != "latest" {
		t.Fatalf("after spec change: expected metadata.tag=latest, got %q", got)
	}
	if got := agentTagCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after spec change: expected 1 replaced tag row, got %d", got)
	}
}

// TestVersioning_MetadataVersionRejected pipes a manifest with
// metadata.version set into `arctl apply -f -` and asserts the CLI rejects
// it with the system-assigned-version error from the v1alpha1 decoder.
func TestVersioning_MetadataVersionRejected(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	name := UniqueAgentName("vermetav")

	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
  version: "1.0.0"
spec:
  source:
    image: ghcr.io/e2e-test/metaversion:latest
  description: "manifest with system-managed metadata.version set"
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, name)

	bin := arctlBinary(t)
	cmd := exec.Command(bin, "apply", "-f", "-", "--registry-url", regURL)
	cmd.Dir = tmpDir
	cmd.Env = os.Environ()
	cmd.Stdin = strings.NewReader(yaml)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if err == nil {
		t.Fatalf("expected non-zero exit when manifest carries metadata.version\nstdout: %s\nstderr: %s",
			stdout.String(), stderr.String())
	}

	combined := stdout.String() + stderr.String()
	if !strings.Contains(combined, "metadata.version") {
		t.Fatalf("expected error mentioning %q, got:\nstdout: %s\nstderr: %s",
			"metadata.version", stdout.String(), stderr.String())
	}

	// Belt-and-braces: the agent must not have been created.
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})
	verifyAgentNotFound(t, regURL, name, "latest")
}

// TestVersioning_DeleteSemantics covers the delete-then-reapply flow against
// the tag contract:
//   - apply an explicit stable tag plus default latest
//   - delete without a tag -> removes only latest; stable remains
//   - delete --all-versions -> removes every tag; name is freed
//   - re-apply -> latest is created again
func TestVersioning_DeleteSemantics(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	name := UniqueAgentName("verdelete")

	// Final cleanup: best-effort wipe in case anything is left over.
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})

	// Step 1: apply stable + latest.
	applyAgentWithTagAndDesc(t, regURL, tmpDir, name, "stable", "stable")
	applyAgentWithDesc(t, regURL, tmpDir, name, "latest")
	if got := agentTagCount(t, regURL, tmpDir, name); got != 2 {
		t.Fatalf("setup: expected 2 tags before delete, got %d", got)
	}
	if got := agentTag(t, regURL, tmpDir, name); got != "latest" {
		t.Fatalf("setup: expected default get to read latest tag, got %q", got)
	}

	// Step 2: delete with no flag -> removes the literal latest tag.
	result := RunArctl(t, tmpDir, "delete", "agent", name, "--registry-url", regURL)
	RequireSuccess(t, result)

	if got := agentTagCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after default delete: expected 1 surviving tag, got %d", got)
	}
	if got := agentTag(t, regURL, tmpDir, name, "--tag", "stable"); got != "stable" {
		t.Fatalf("after default delete: expected explicit stable tag to remain, got %q", got)
	}
	getLatest := RunArctl(t, tmpDir, "get", "agent", name, "-o", "json", "--registry-url", regURL)
	if combined := getLatest.Stdout + getLatest.Stderr; !strings.Contains(combined, "not found") {
		t.Fatalf("after default delete: expected default latest get to be not found, got:\nstdout: %s\nstderr: %s",
			getLatest.Stdout, getLatest.Stderr)
	}

	// Step 3: delete --all-versions -> drops every tag.
	result = RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	RequireSuccess(t, result)

	// The name must be free now: get returns "not found".
	getResult := RunArctl(t, tmpDir, "get", "agent", name, "-o", "json", "--registry-url", regURL)
	combined := getResult.Stdout + getResult.Stderr
	if !strings.Contains(combined, "not found") {
		t.Fatalf("after --all-versions delete: expected 'not found', got:\nstdout: %s\nstderr: %s",
			getResult.Stdout, getResult.Stderr)
	}

	// Step 4: re-apply -> latest is created again.
	applyAgentWithDesc(t, regURL, tmpDir, name, "after-reset")
	if got := agentTag(t, regURL, tmpDir, name); got != "latest" {
		t.Fatalf("after reset re-apply: expected metadata.tag=latest, got %q", got)
	}
	if got := agentTagCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after reset re-apply: expected 1 tag row, got %d", got)
	}
}

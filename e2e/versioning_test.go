//go:build e2e

// Tests for the immutable-resource-versioning contract:
//   - apply assigns sequential integer versions starting at 1
//   - re-applying an unchanged spec is a no-op (no new version row)
//   - applying a changed spec bumps to the next integer
//   - manifests with metadata.version set are rejected at decode time
//   - delete defaults to latest; --all-versions clears the entire history
//     and frees the name (numbering resets on next apply)
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

// applyAgent applies an Agent YAML for `name` whose spec contains a single
// description field set to `desc`. Returns the test-fatal-on-error result.
func applyAgentWithDesc(t *testing.T, regURL, tmpDir, name, desc string) {
	t.Helper()
	yaml := fmt.Sprintf(`apiVersion: ar.dev/v1alpha1
kind: Agent
metadata:
  name: %s
spec:
  source:
    image: ghcr.io/e2e-test/versioning-agent:latest
  description: %q
  language: python
  framework: adk
  modelProvider: gemini
  modelName: gemini-2.0-flash
`, name, desc)
	path := writeDeclarativeYAML(t, tmpDir, fmt.Sprintf("agent-%d.yaml", time.Now().UnixNano()), yaml)
	result := RunArctl(t, tmpDir, "apply", "-f", path, "--registry-url", regURL)
	RequireSuccess(t, result)
	RequireOutputContains(t, result, "Agent/"+name)
}

// agentStatusVersion runs `arctl get agent <name> -o json --registry-url ...`
// and returns the integer at status.version. Fails the test if the call
// fails or the value is missing.
func agentStatusVersion(t *testing.T, regURL, tmpDir, name string) int {
	t.Helper()
	result := RunArctl(t, tmpDir, "get", "agent", name, "-o", "json", "--registry-url", regURL)
	RequireSuccess(t, result)
	var decoded struct {
		Status struct {
			Version int `json:"version"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &decoded); err != nil {
		t.Fatalf("decode get -o json: %v\nstdout: %s", err, result.Stdout)
	}
	return decoded.Status.Version
}

// agentVersionCount runs `arctl get agent <name> --all-versions -o json` and
// returns the number of rows returned. The CLI emits a JSON array of typed
// Agent objects.
func agentVersionCount(t *testing.T, regURL, tmpDir, name string) int {
	t.Helper()
	result := RunArctl(t, tmpDir, "get", "agent", name,
		"--all-versions", "-o", "json", "--registry-url", regURL)
	RequireSuccess(t, result)
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(result.Stdout), &arr); err != nil {
		t.Fatalf("decode get --all-versions -o json: %v\nstdout: %s", err, result.Stdout)
	}
	return len(arr)
}

// TestVersioning_ApplyAndIdempotency verifies the core immutable-versioning
// contract on the apply path:
//   - first apply lands as v1 (status.version == 1, exactly one row)
//   - re-applying the identical spec is a no-op (still one row at v1)
//   - applying a changed spec bumps to v2 (two rows; latest is v2)
func TestVersioning_ApplyAndIdempotency(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()

	name := UniqueAgentName("verapply")

	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})

	// Step 1: first apply → v1.
	applyAgentWithDesc(t, regURL, tmpDir, name, "first")
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after first apply: expected status.version=1, got %d", got)
	}
	if got := agentVersionCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after first apply: expected 1 version row, got %d", got)
	}

	// Step 2: same spec → idempotent (no new row).
	applyAgentWithDesc(t, regURL, tmpDir, name, "first")
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after re-apply: expected status.version=1, got %d", got)
	}
	if got := agentVersionCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after re-apply: expected 1 version row, got %d", got)
	}

	// Step 3: changed spec → v2.
	applyAgentWithDesc(t, regURL, tmpDir, name, "second")
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 2 {
		t.Fatalf("after spec change: expected status.version=2, got %d", got)
	}
	if got := agentVersionCount(t, regURL, tmpDir, name); got != 2 {
		t.Fatalf("after spec change: expected 2 version rows, got %d", got)
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
	if !strings.Contains(combined, "metadata.version is system-assigned") {
		t.Fatalf("expected error mentioning %q, got:\nstdout: %s\nstderr: %s",
			"metadata.version is system-assigned", stdout.String(), stderr.String())
	}

	// Belt-and-braces: the agent must not have been created.
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})
	verifyAgentNotFound(t, regURL, name, "1")
}

// TestVersioning_DeleteSemantics covers the delete-then-reapply flow
// against the immutable-versioning contract:
//   - apply v1 + v2 (changed spec)
//   - delete without flag → removes only the latest (v2); v1 remains
//   - delete --all-versions → removes everything; name is freed
//   - re-apply → numbering resets (the new row is v1, not v3)
func TestVersioning_DeleteSemantics(t *testing.T) {
	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	name := UniqueAgentName("verdelete")

	// Final cleanup: best-effort wipe in case anything is left over.
	t.Cleanup(func() {
		RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	})

	// Step 1: apply v1 + v2.
	applyAgentWithDesc(t, regURL, tmpDir, name, "first")
	applyAgentWithDesc(t, regURL, tmpDir, name, "second")
	if got := agentVersionCount(t, regURL, tmpDir, name); got != 2 {
		t.Fatalf("setup: expected 2 versions before delete, got %d", got)
	}
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 2 {
		t.Fatalf("setup: expected status.version=2 before delete, got %d", got)
	}

	// Step 2: delete with no flag → removes the latest version (v2).
	// `arctl delete agent NAME` defaults to the latest live version.
	result := RunArctl(t, tmpDir, "delete", "agent", name, "--registry-url", regURL)
	RequireSuccess(t, result)

	if got := agentVersionCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after default delete: expected 1 surviving version, got %d", got)
	}
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after default delete: expected status.version=1 (v2 removed, v1 promoted), got %d", got)
	}

	// Step 3: delete --all-versions → drops every version.
	result = RunArctl(t, tmpDir, "delete", "agent", name, "--all-versions", "--registry-url", regURL)
	RequireSuccess(t, result)

	// The name must be free now: get returns "not found".
	getResult := RunArctl(t, tmpDir, "get", "agent", name, "-o", "json", "--registry-url", regURL)
	combined := getResult.Stdout + getResult.Stderr
	if !strings.Contains(combined, "not found") {
		t.Fatalf("after --all-versions delete: expected 'not found', got:\nstdout: %s\nstderr: %s",
			getResult.Stdout, getResult.Stderr)
	}

	// Step 4: re-apply → numbering resets to v1.
	applyAgentWithDesc(t, regURL, tmpDir, name, "after-reset")
	if got := agentStatusVersion(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after reset re-apply: expected status.version=1, got %d", got)
	}
	if got := agentVersionCount(t, regURL, tmpDir, name); got != 1 {
		t.Fatalf("after reset re-apply: expected 1 version row, got %d", got)
	}
}


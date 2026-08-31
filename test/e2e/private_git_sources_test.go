//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"text/template"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

const (
	privateGitFixtureName      = "git-fixture"
	privateGitFixtureNamespace = "agentregistry"
	privateGitInvalidPassword  = "invalid-e2e-token"
)

func TestPrivateGitCatalogSources(t *testing.T) {
	t.Logf("Setting up private Git fixture %s/%s", privateGitFixtureNamespace, privateGitFixtureName)
	requirePrivateGitFixture(t)

	regURL := RegistryURL(t)
	tmpDir := t.TempDir()
	secretName := UniqueNameWithPrefix("e2e-git-creds")
	invalidSecretName := UniqueNameWithPrefix("e2e-git-invalid-creds")
	pluginAuthName := UniqueNameWithPrefix("e2e-plugin-auth")
	pluginInvalidAuthName := UniqueNameWithPrefix("e2e-plugin-invalid-auth")
	pluginAnonName := UniqueNameWithPrefix("e2e-plugin-anon")
	skillAuthName := UniqueNameWithPrefix("e2e-skill-auth")
	skillAnonName := UniqueNameWithPrefix("e2e-skill-anon")

	path := renderPrivateGitSources(t, tmpDir, map[string]string{
		"SecretName":            secretName,
		"InvalidSecretName":     invalidSecretName,
		"PluginAuthName":        pluginAuthName,
		"PluginInvalidAuthName": pluginInvalidAuthName,
		"PluginAnonName":        pluginAnonName,
		"SkillAuthName":         skillAuthName,
		"SkillAnonName":         skillAnonName,
	})
	t.Cleanup(func() {
		t.Logf("Deleting private Git catalog resources from %s", path)
		RunArctl(t, tmpDir, "delete", "-f", path)
	})
	t.Logf("Applying private Git catalog resources from %s", path)
	RequireSuccess(t, RunArctl(t, tmpDir, "apply", "-f", path))

	t.Logf("Verifying authenticated Plugin %q resolves the private source", pluginAuthName)
	plugin, pluginRaw := waitForPluginReady(t, regURL, pluginAuthName, v1alpha1.ConditionTrue)
	require.NotNil(t, plugin.Status.ResolvedSource, "plugin never recorded a resolved source: %s", pluginRaw)
	require.Len(t, plugin.Status.ResolvedSource.Commit, 40)
	require.NotNil(t, plugin.Status.Manifest)
	assert.Equal(t, "private-plugin", plugin.Status.Manifest.Name)

	t.Logf("Verifying authenticated Skill %q resolves the private source", skillAuthName)
	skill, skillRaw := waitForSkillReady(t, regURL, skillAuthName, v1alpha1.ConditionTrue)
	require.NotNil(t, skill.Status.ResolvedSource, "skill never recorded a resolved source: %s", skillRaw)
	require.Len(t, skill.Status.ResolvedSource.Commit, 40)
	assert.Equal(t, plugin.Status.ResolvedSource.Commit, skill.Status.ResolvedSource.Commit)

	t.Logf("Verifying anonymous Plugin %q cannot resolve the private source", pluginAnonName)
	pluginAnon, _ := waitForPluginReady(t, regURL, pluginAnonName, v1alpha1.ConditionFalse)
	assert.Equal(t, "SourceUnresolvable", pluginAnon.Status.GetCondition("Ready").Reason)
	t.Logf("Verifying anonymous Skill %q cannot resolve the private source", skillAnonName)
	skillAnon, _ := waitForSkillReady(t, regURL, skillAnonName, v1alpha1.ConditionFalse)
	assert.Equal(t, "SourceUnresolvable", skillAnon.Status.GetCondition("Ready").Reason)

	t.Logf("Verifying failed authentication for Plugin %q redacts credentials", pluginInvalidAuthName)
	pluginInvalidAuth, pluginInvalidAuthRaw := waitForPluginReady(
		t,
		regURL,
		pluginInvalidAuthName,
		v1alpha1.ConditionFalse,
	)
	invalidAuthCondition := pluginInvalidAuth.Status.GetCondition("Ready")
	assert.Equal(t, "SourceUnresolvable", invalidAuthCondition.Reason)
	assert.Contains(t, strings.ToLower(invalidAuthCondition.Message), "authentication failed")
	assert.NotContains(t, pluginInvalidAuthRaw, privateGitInvalidPassword)
	assert.Contains(t, pluginInvalidAuthRaw, "xxxxx")
}

func renderPrivateGitSources(t *testing.T, outputDir string, data map[string]string) string {
	t.Helper()
	manifest := privateGitTestdata("private_git_sources.yaml.tmpl")
	tmpl, err := template.New(filepath.Base(manifest)).Option("missingkey=error").ParseFiles(manifest)
	require.NoError(t, err)

	var rendered bytes.Buffer
	require.NoError(t, tmpl.Execute(&rendered, data))
	return writeDeclarativeYAML(t, outputDir, "private-git-sources.yaml", rendered.String())
}

func waitForPluginReady(t *testing.T, regURL, name string, want v1alpha1.ConditionStatus) (*v1alpha1.Plugin, string) {
	t.Helper()
	var plugin *v1alpha1.Plugin
	var raw string
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		plugin, raw = getPrivateGitResource(t, regURL, "plugins", name, defaultArtifactTag, func() *v1alpha1.Plugin {
			return &v1alpha1.Plugin{}
		})
		if plugin != nil && plugin.Status.GetCondition("Ready") != nil && plugin.Status.GetCondition("Ready").Status == want {
			return plugin, raw
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("plugin %s never reached Ready=%s (last response: %s)", name, want, raw)
	return nil, ""
}

func waitForSkillReady(t *testing.T, regURL, name string, want v1alpha1.ConditionStatus) (*v1alpha1.Skill, string) {
	t.Helper()
	var skill *v1alpha1.Skill
	var raw string
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		skill, raw = getPrivateGitResource(t, regURL, "skills", name, defaultArtifactTag, func() *v1alpha1.Skill {
			return &v1alpha1.Skill{}
		})
		if skill != nil && skill.Status.GetCondition("Ready") != nil && skill.Status.GetCondition("Ready").Status == want {
			return skill, raw
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("skill %s never reached Ready=%s (last response: %s)", name, want, raw)
	return nil, ""
}

func getPrivateGitResource[T any](t *testing.T, regURL, resource, name, tag string, newObject func() *T) (*T, string) {
	t.Helper()
	resp := RegistryGet(t, resourceURL(regURL, resource, name, tag))
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	if resp.StatusCode != http.StatusOK {
		return nil, string(body)
	}
	object := newObject()
	require.NoError(t, json.Unmarshal(body, object))
	return object, string(body)
}

func requirePrivateGitFixture(t *testing.T) {
	t.Helper()
	manifest := privateGitTestdata("private_git_fixture.yaml")
	cmd := exec.Command("kubectl", "--context", KubeContext, "apply", "-f", manifest)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "apply private Git fixture: %s", string(out))

	t.Cleanup(func() {
		t.Logf("Deleting private Git fixture from %s", manifest)
		cmd := exec.Command(
			"kubectl", "--context", KubeContext,
			"delete", "-f", manifest,
			"--ignore-not-found", "--wait=false",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Logf("delete private Git fixture: %v (output: %s)", err, string(out))
		}
	})

	cmd = exec.Command(
		"kubectl", "--context", KubeContext,
		"-n", privateGitFixtureNamespace,
		"rollout", "status", "deployment/"+privateGitFixtureName,
		"--timeout=120s",
	)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "private Git fixture never became ready: %s", string(out))

	t.Logf("Waiting for private Git fixture Service endpoints")
	cmd = exec.Command(
		"kubectl", "--context", KubeContext,
		"-n", privateGitFixtureNamespace,
		"wait", "--for=jsonpath={.subsets[0].addresses[0].ip}",
		"endpoints/"+privateGitFixtureName,
		"--timeout=120s",
	)
	out, err = cmd.CombinedOutput()
	require.NoError(t, err, "private Git fixture Service has no ready endpoints: %s", string(out))
}

func privateGitTestdata(name string) string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		panic("resolve private Git testdata")
	}
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

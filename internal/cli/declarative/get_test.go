package declarative_test

import (
	"testing"

	"github.com/agentregistry-dev/agentregistry/internal/cli/declarative"
	"github.com/agentregistry-dev/agentregistry/internal/cli/scheme"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetCmd_RejectsUnknownType(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"unknowntype"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "unknown kind")
}

func TestGetCmd_RequiresTypeArg(t *testing.T) {
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{})
	err := cmd.Execute()
	assert.Error(t, err)
}

func TestGetCmd_NoAPIClientErrors(t *testing.T) {
	declarative.SetAPIClient(nil)
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized")
}

// TestGetCmd_RegistryDrivenColumnLookup verifies the package-level scheme
// registry resolves declarative-known kinds (declarative's init() registered
// them at process start), so `arctl get agents` gets past kind validation
// and fails only at the API-client check.
func TestGetCmd_RegistryDrivenColumnLookup(t *testing.T) {
	k, err := scheme.Lookup("agents")
	require.NoError(t, err, "agents alias should resolve via declarative's init() registration")
	assert.NotEmpty(t, k.TableColumns, "expected TableColumns on the agent kind")

	declarative.SetAPIClient(nil)

	// Looking up a valid kind should get past kind validation and fail
	// only at "API client not initialized" — confirming the dispatch ran.
	cmd := declarative.NewGetCmd()
	cmd.SetArgs([]string{"agents"})
	err = cmd.Execute()
	require.Error(t, err)
	assert.ErrorContains(t, err, "API client not initialized",
		"should fail at API client check, not kind lookup")
}

// TestProvider_NoAllVersionsSupport pins that Provider — a legacy,
// single-version-identity kind — is registered without ListVersions /
// DeleteAllVersions closures. The dispatch layer rejects --all-versions
// when those fields are nil, which is exactly the behavior we want for
// Provider on this branch (its server store has no /versions endpoint).
func TestProvider_NoAllVersionsSupport(t *testing.T) {
	k, err := scheme.Lookup("provider")
	require.NoError(t, err)
	require.Nil(t, k.ListVersions, "Provider should not expose ListVersions (legacy kind)")
	require.Nil(t, k.DeleteAllVersions, "Provider should not expose DeleteAllVersions (legacy kind)")
}

// TestDeployment_NoAllVersionsSupport is the symmetric assertion for
// Deployment — also a legacy single-version-identity kind. Already
// covered by TestGet_AllVersions_DeploymentRejected at the CLI surface
// but pinning it at the registry shape level guards against an
// accidental ListVersions wiring regression.
func TestDeployment_NoAllVersionsSupport(t *testing.T) {
	k, err := scheme.Lookup("deployment")
	require.NoError(t, err)
	require.Nil(t, k.ListVersions, "Deployment should not expose ListVersions (legacy kind)")
	require.Nil(t, k.DeleteAllVersions, "Deployment should not expose DeleteAllVersions (legacy kind)")
}

package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResourceRef_NormalizesDeprecatedVersionAlias(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		var ref ResourceRef
		require.NoError(t, json.Unmarshal([]byte(`{"kind":"Agent","name":"alice","version":"stable"}`), &ref))
		require.Equal(t, "stable", ref.Tag)
		require.Empty(t, ref.Version)

		out, err := json.Marshal(ref)
		require.NoError(t, err)
		require.JSONEq(t, `{"kind":"Agent","name":"alice","tag":"stable"}`, string(out))
		require.NotContains(t, string(out), "version")
	})

	t.Run("yaml", func(t *testing.T) {
		var ref ResourceRef
		require.NoError(t, yaml.Unmarshal([]byte("kind: Agent\nname: alice\nversion: stable\n"), &ref))
		require.Equal(t, "stable", ref.Tag)
		require.Empty(t, ref.Version)

		out, err := yaml.Marshal(ref)
		require.NoError(t, err)
		require.Contains(t, string(out), "tag: stable")
		require.NotContains(t, string(out), "version")
	})
}

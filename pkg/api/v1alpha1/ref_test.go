package v1alpha1

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func TestResourceRef_RejectsDeprecatedVersionAlias(t *testing.T) {
	for _, key := range []string{"version", "Version"} {
		t.Run("json "+key, func(t *testing.T) {
			var ref ResourceRef
			err := json.Unmarshal([]byte(`{"kind":"Agent","name":"alice","`+key+`":"stable"}`), &ref)
			require.Error(t, err)
			require.Contains(t, err.Error(), "version is deprecated; use tag")
		})

		t.Run("yaml "+key, func(t *testing.T) {
			var ref ResourceRef
			err := yaml.Unmarshal([]byte("kind: Agent\nname: alice\n"+key+": stable\n"), &ref)
			require.Error(t, err)
			require.Contains(t, err.Error(), "version is deprecated; use tag")
		})
	}
}

func TestResourceRef_UsesTag(t *testing.T) {
	t.Run("json", func(t *testing.T) {
		var ref ResourceRef
		require.NoError(t, json.Unmarshal([]byte(`{"kind":"Agent","name":"alice","tag":"stable"}`), &ref))
		require.Equal(t, "stable", ref.Tag)

		out, err := json.Marshal(ref)
		require.NoError(t, err)
		require.JSONEq(t, `{"kind":"Agent","name":"alice","tag":"stable"}`, string(out))
		require.NotContains(t, string(out), "version")
	})

	t.Run("yaml", func(t *testing.T) {
		var ref ResourceRef
		require.NoError(t, yaml.Unmarshal([]byte("kind: Agent\nname: alice\ntag: stable\n"), &ref))
		require.Equal(t, "stable", ref.Tag)

		out, err := yaml.Marshal(ref)
		require.NoError(t, err)
		require.Contains(t, string(out), "tag: stable")
		require.NotContains(t, string(out), "version")
	})
}

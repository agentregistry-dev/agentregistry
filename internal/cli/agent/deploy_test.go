package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDeployCmd_WaitFlag(t *testing.T) {
	f := DeployCmd.Flags().Lookup("wait")
	require.NotNil(t, f, "--wait flag should be registered")
	assert.Equal(t, "true", f.DefValue)
	assert.Equal(t, "bool", f.Value.Type())
}

func TestDeployCmd_Flags(t *testing.T) {
	tests := []struct {
		name     string
		flag     string
		wantType string
	}{
		{"version", "version", "string"},
		{"provider-id", "provider-id", "string"},
		{"namespace", "namespace", "string"},
		{"prefer-remote", "prefer-remote", "bool"},
		{"wait", "wait", "bool"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := DeployCmd.Flags().Lookup(tt.flag)
			require.NotNil(t, f, "--%s flag should be registered", tt.flag)
			assert.Equal(t, tt.wantType, f.Value.Type())
		})
	}
}

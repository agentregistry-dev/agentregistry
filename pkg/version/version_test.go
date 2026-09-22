package version_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/agentregistry-dev/agentregistry/pkg/version"
)

func TestEnsureVPrefix(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "adds prefix", value: "1.2.3", want: "v1.2.3"},
		{name: "keeps prefix", value: "v1.2.3", want: "v1.2.3"},
		{name: "empty value", value: "", want: "v"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, version.EnsureVPrefix(tt.value))
		})
	}
}

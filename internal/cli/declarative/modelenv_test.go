package declarative

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestModelProviderEnvKeys(t *testing.T) {
	cases := []struct {
		provider string
		want     []string
	}{
		{"gemini", []string{"GOOGLE_API_KEY"}},
		{"GEMINI", []string{"GOOGLE_API_KEY"}}, // case-insensitive
		{"openai", []string{"OPENAI_API_KEY"}},
		{"anthropic", []string{"ANTHROPIC_API_KEY"}},
		{"bedrock", []string{"AWS_ACCESS_KEY_ID", "AWS_SECRET_ACCESS_KEY", "AWS_REGION"}},
		{"agentgateway", nil},
		{"unknown", nil},
		{"", nil},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			assert.Equal(t, tc.want, ModelProviderEnvKeys(tc.provider))
		})
	}
}

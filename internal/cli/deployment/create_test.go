package deployment

import (
	"os"
	"strings"
	"testing"

	"github.com/agentregistry-dev/agentregistry/pkg/api/v1alpha1"
)

func TestValidateAPIKey_WithExtraEnv(t *testing.T) {
	tests := []struct {
		name          string
		modelProvider string
		osEnv         map[string]string
		extraEnv      map[string]string
		wantErr       bool
		errContain    string
	}{
		{
			name:          "key in extra env only",
			modelProvider: "gemini",
			osEnv:         map[string]string{},
			extraEnv:      map[string]string{"GOOGLE_API_KEY": "test-key"},
			wantErr:       false,
		},
		{
			name:          "key in os env only",
			modelProvider: "openai",
			osEnv:         map[string]string{"OPENAI_API_KEY": "sk-test"},
			extraEnv:      map[string]string{},
			wantErr:       false,
		},
		{
			name:          "key missing from both",
			modelProvider: "anthropic",
			osEnv:         map[string]string{},
			extraEnv:      map[string]string{},
			wantErr:       true,
			errContain:    "ANTHROPIC_API_KEY",
		},
		{
			name:          "nil extra env falls back to os",
			modelProvider: "openai",
			osEnv:         map[string]string{"OPENAI_API_KEY": "sk-test"},
			wantErr:       false,
		},
		{
			name:          "unknown provider always passes",
			modelProvider: "custom-llm",
			osEnv:         map[string]string{},
			extraEnv:      map[string]string{},
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, envVar := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AZUREOPENAI_API_KEY"} {
				os.Unsetenv(envVar)
			}
			for k, v := range tt.osEnv {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.osEnv {
					os.Unsetenv(k)
				}
			}()

			err := validateAPIKey(tt.modelProvider, tt.extraEnv)

			if (err != nil) != tt.wantErr {
				t.Errorf("validateAPIKey(%q) error = %v, wantErr %v", tt.modelProvider, err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContain != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContain) {
					t.Errorf("validateAPIKey(%q) error = %v, want error containing %q", tt.modelProvider, err, tt.errContain)
				}
			}
		})
	}
}

func TestBuildAgentDeployConfig_WithEnvOverrides(t *testing.T) {
	tests := []struct {
		name         string
		manifest     *v1alpha1.Agent
		envOverrides map[string]string
		osEnv        map[string]string
		wantKeys     map[string]string
		wantAbsent   []string
	}{
		{
			name: "env override included in config",
			manifest: &v1alpha1.Agent{
				Spec: v1alpha1.AgentSpec{
					ModelProvider: "gemini",
				},
			},
			envOverrides: map[string]string{"GOOGLE_API_KEY": "from-flag", "CUSTOM_VAR": "custom-val"},
			wantKeys:     map[string]string{"GOOGLE_API_KEY": "from-flag", "CUSTOM_VAR": "custom-val"},
		},
		{
			name: "env override takes precedence over os env",
			manifest: &v1alpha1.Agent{
				Spec: v1alpha1.AgentSpec{
					ModelProvider: "openai",
				},
			},
			osEnv:        map[string]string{"OPENAI_API_KEY": "from-os"},
			envOverrides: map[string]string{"OPENAI_API_KEY": "from-flag"},
			wantKeys:     map[string]string{"OPENAI_API_KEY": "from-flag"},
		},
		{
			name: "os env used when no override",
			manifest: &v1alpha1.Agent{
				Spec: v1alpha1.AgentSpec{
					ModelProvider: "openai",
				},
			},
			osEnv:        map[string]string{"OPENAI_API_KEY": "from-os"},
			envOverrides: map[string]string{},
			wantKeys:     map[string]string{"OPENAI_API_KEY": "from-os"},
		},
		{
			name: "telemetry endpoint included",
			manifest: &v1alpha1.Agent{
				Spec: v1alpha1.AgentSpec{
					ModelProvider:     "openai",
					TelemetryEndpoint: "http://otel:4317",
				},
			},
			envOverrides: map[string]string{"OPENAI_API_KEY": "key"},
			wantKeys:     map[string]string{"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT": "http://otel:4317", "OPENAI_API_KEY": "key"},
		},
		{
			name: "empty overrides with no os env",
			manifest: &v1alpha1.Agent{
				Spec: v1alpha1.AgentSpec{
					ModelProvider: "gemini",
				},
			},
			envOverrides: map[string]string{},
			wantAbsent:   []string{"GOOGLE_API_KEY"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, envVar := range []string{"OPENAI_API_KEY", "ANTHROPIC_API_KEY", "GOOGLE_API_KEY", "AZUREOPENAI_API_KEY"} {
				os.Unsetenv(envVar)
			}
			for k, v := range tt.osEnv {
				os.Setenv(k, v)
			}
			defer func() {
				for k := range tt.osEnv {
					os.Unsetenv(k)
				}
			}()

			config := buildAgentDeployConfig(tt.manifest, tt.envOverrides)

			for k, v := range tt.wantKeys {
				if got, ok := config[k]; !ok {
					t.Errorf("expected config key %q, not found", k)
				} else if got != v {
					t.Errorf("config[%q] = %q, want %q", k, got, v)
				}
			}
			for _, k := range tt.wantAbsent {
				if _, ok := config[k]; ok {
					t.Errorf("expected config key %q to be absent, but found it", k)
				}
			}
		})
	}
}

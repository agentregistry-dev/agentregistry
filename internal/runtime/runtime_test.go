package runtime

import (
	"fmt"
	"testing"
)

func TestValidateRuntime(t *testing.T) {
	tests := []struct {
		name       string
		runtime    string
		wantErr    bool
		customFunc RuntimeValidator
	}{
		{
			name:    "valid local runtime",
			runtime: "local",
			wantErr: false,
		},
		{
			name:    "valid kubernetes runtime",
			runtime: "kubernetes",
			wantErr: false,
		},
		{
			name:    "invalid runtime without custom validator",
			runtime: "runtimeA",
			wantErr: true,
		},
		{
			name:    "invalid runtime with custom validator that doesn't accept it",
			runtime: "unknown",
			wantErr: true,
			customFunc: func(runtime string) error {
				return fmt.Errorf("unsupported custom runtime: %s", runtime)
			},
		},
		{
			name:    "valid runtime with custom validator accepting vertex",
			runtime: "runtimeA",
			wantErr: false,
			customFunc: func(runtime string) error {
				if runtime == "runtimeA" {
					return nil
				}
				return fmt.Errorf("unsupported custom runtime: %s", runtime)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalValidator := CustomRuntimeValidator
			defer func() {
				CustomRuntimeValidator = originalValidator
			}()

			CustomRuntimeValidator = tt.customFunc

			err := ValidateRuntime(tt.runtime)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateRuntime() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSupportedRuntimes(t *testing.T) {
	expected := []string{"local", "kubernetes"}

	if len(SupportedRuntimes) != len(expected) {
		t.Errorf("SupportedRuntimes length = %d, want %d", len(SupportedRuntimes), len(expected))
	}

	for i, runtime := range expected {
		if SupportedRuntimes[i] != runtime {
			t.Errorf("SupportedRuntimes[%d] = %s, want %s", i, SupportedRuntimes[i], runtime)
		}
	}
}

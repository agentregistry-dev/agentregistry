package config

import (
	"testing"
)

func TestValidate_EmbeddingsDimensions(t *testing.T) {
	tests := []struct {
		name       string
		dimensions int
		wantErr    bool
		errMsg     string
	}{
		{"valid default 1536", 1536, false, ""},
		{"valid 1024 (Voyage AI)", 1024, false, ""},
		{"valid 3072 (OpenAI large)", 3072, false, ""},
		{"valid 768", 768, false, ""},
		{"zero dimensions", 0, true, "must be positive"},
		{"negative dimensions", -1, true, "must be positive"},
		{"exceeds max", 16385, true, "must not exceed 16384"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				Embeddings: EmbeddingsConfig{
					Enabled:    true,
					Dimensions: tt.dimensions,
					Model:      "test-model",
					Provider:   "openai",
				},
			}
			err := Validate(cfg)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Validate() expected error containing %q, got nil", tt.errMsg)
				} else if tt.errMsg != "" {
					if !containsSubstring(err.Error(), tt.errMsg) {
						t.Errorf("Validate() error = %q, want substring %q", err.Error(), tt.errMsg)
					}
				}
			} else {
				if err != nil {
					t.Errorf("Validate() unexpected error: %v", err)
				}
			}
		})
	}
}

func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
